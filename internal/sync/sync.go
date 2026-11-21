// Package sync keeps the listener's data the same across their devices,
// through a private GitHub repository: what changed here is pushed, what
// changed elsewhere is pulled, each file taken from whichever side wrote it
// last. On pull, local-library playlists merge by ID and likes are combined.
package sync

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Unit is a kind of data the listener chooses to sync, each on its own.
type Unit string

const (
	UnitSettings  Unit = "settings"
	UnitPlugins   Unit = "plugins"
	UnitPlaylists Unit = "playlists"
)

// Units are all the kinds, in the order the settings show them.
var Units = []Unit{UnitSettings, UnitPlaylists, UnitPlugins}

// Config is what the listener set up: their token, the repository's name
// under their account, and which kinds sync.
type Config struct {
	Token                 string        `json:"token"`
	RefreshToken          string        `json:"refreshToken,omitempty"`
	TokenExpiresAt        int64         `json:"tokenExpiresAt,omitempty"`
	RefreshTokenExpiresAt int64         `json:"refreshTokenExpiresAt,omitempty"`
	Repo                  string        `json:"repo"`
	Units                 map[Unit]bool `json:"units"`
}

// Enabled reports whether anything syncs: a token, a repo, and a kind on.
func (c Config) Enabled() bool {
	if c.Token == "" || c.Repo == "" {
		return false
	}
	for _, u := range Units {
		if c.Units[u] {
			return true
		}
	}
	return false
}

// File is one file as the manifest holds it: where it is, what it holds, and
// when it last changed, as Unix nanoseconds.
type File struct {
	Object    bool   `json:"object,omitempty"`
	Path      string `json:"path"`
	Sha       string `json:"sha"`
	UpdatedAt int64  `json:"updatedAt"`
	Unit      Unit   `json:"unit"`
}

// Manifest is what the repository says of every synced file.
type Manifest struct {
	Version int             `json:"version"`
	Files   map[string]File `json:"files"`
}

// Store is the repository synced with: GitHub's contents API in the app, a
// fake in tests. Paths are slash-separated, relative to the repository.
type Store interface {
	CollectionStore
	// EnsureRepo makes the private repository if it isn't there yet.
	EnsureRepo() error
	// Get returns a file's bytes, or nil if it isn't there.
	Get(path string) ([]byte, error)
	// Put writes a file, creating or replacing it.
	Put(path string, b []byte) error
}

// manifestPath is where the manifest sits in the repository; dataPrefix is
// what every synced file's path starts with, keeping them apart from it.
const (
	manifestPath = "manifest.json"
	dataPrefix   = "data/"
)

// Engine syncs a device's data directory with a store.
type Engine struct {
	Progress func(phase string, done, total int)
	Changed  []string
	RepoID   string
	Defaults []byte
	Root     string // the user data directory
	Store    Store
}

// localFile is a file on this device, offered to sync.
type localFile struct {
	rel     string // slash-separated, relative to Root
	unit    Unit
	modTime int64
	content []byte
}

func (e *Engine) report(phase string, done, total int) {
	if e.Progress != nil {
		e.Progress(phase, done, total)
	}
}

// SyncFiles publishes one manifest revision after all its immutable file
// contents exist. Two devices cannot silently overwrite each other's commit.
func (e *Engine) SyncFiles(units map[Unit]bool) error {
	for _, unit := range []Unit{UnitSettings, UnitPlugins} {
		if units[unit] {
			if err := e.syncUnit(unit); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) syncUnit(unit Unit) error {
	path := "files-v2/" + string(unit) + ".json"
	raw, rev, err := e.Store.ReadVersion(path)
	if err != nil {
		return err
	}
	legacy := raw == nil
	if legacy {
		raw, err = e.Store.Get(manifestPath)
		if err != nil {
			return err
		}
	}
	var m Manifest
	if raw != nil {
		if err = json.Unmarshal(raw, &m); err != nil {
			return err
		}
	}
	if m.Version > 2 {
		return fmt.Errorf("unsupported sync manifest version %d", m.Version)
	}
	if m.Files == nil {
		m.Files = map[string]File{}
	}
	m.Version = 2
	checkpoint := filepath.Join(e.Root, "sync-history", fmt.Sprintf("%x", sha256.Sum256([]byte(e.RepoID))), "preferences-seen")
	var last string
	first := false
	if unit == UnitSettings {
		b, err := os.ReadFile(checkpoint)
		last = string(b)
		first = os.IsNotExist(err)
		if err != nil && !first {
			return err
		}
	}
	locals, err := e.localFiles(map[Unit]bool{unit: true})
	if err != nil {
		return err
	}
	byPath := map[string]localFile{}
	var paths []string
	for _, lf := range locals {
		byPath[lf.rel] = lf
		paths = append(paths, lf.rel)
	}
	for path, rf := range m.Files {
		if legacy && rf.Unit != unit || strings.HasPrefix(path, "plugin-data/") && strings.HasSuffix(path, "/settings.json") {
			delete(m.Files, path)
			continue
		}
		if rf.Unit != unit || rf.Sha == "" || rf.Path != path || !allowedFile(path, unit) {
			return fmt.Errorf("unexpected sync file %q", path)
		}
		if _, ok := byPath[path]; !ok {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	changed := false
	for i, path := range paths {
		e.report("Checking remote files", i, len(paths))
		lf, exists := byPath[path]
		rf := m.Files[path]
		if rf.Object && exists && rf.Sha == lf.sha() {
			continue
		}
		chosen := lf
		remoteWins := lf.modTime <= rf.UpdatedAt
		if last != "" && exists {
			if rf.Sha == last {
				remoteWins = false
			} else if lf.sha() == last {
				remoteWins = true
			}
		}
		if rf.Sha != "" && (!exists || remoteWins || first) {
			remote, err := e.remoteFile(path, rf)
			if err != nil {
				return err
			}
			chosen = localFile{rel: path, unit: unit, modTime: rf.UpdatedAt, content: remote}
			if first && exists {
				chosen.content, err = initialPreferences(lf.content, remote, e.Defaults, lf.modTime > rf.UpdatedAt)
				if err != nil {
					return err
				}
				chosen.modTime = max(lf.modTime, rf.UpdatedAt)
			}
			if !exists || !bytes.Equal(chosen.content, lf.content) {
				if err := e.applyFile(chosen, lf, exists); err != nil {
					return err
				}
			}
			if rf.Object && chosen.sha() == rf.Sha {
				continue
			}
		}
		e.report("Uploading local changes", i, len(paths))
		sha := chosen.sha()
		key, err := objectPath(sha)
		if err != nil {
			return err
		}
		if err = e.Store.Put(key, chosen.content); err != nil {
			return err
		}
		m.Files[path] = File{Path: path, Unit: unit, Sha: sha, UpdatedAt: chosen.modTime, Object: true}
		changed = true
	}
	e.report("Checking remote files", len(paths), len(paths))
	if changed {
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		e.report("Saving the sync manifest…", 0, 0)
		if err = e.Store.PutVersion(path, data, rev); err != nil {
			return err
		}
	}
	if unit == UnitSettings {
		if sum := m.Files["settings.json"].Sha; sum != "" && sum != last {
			return writeAtomic(checkpoint, []byte(sum))
		}
	}
	return nil
}

func (e *Engine) remoteFile(path string, rf File) ([]byte, error) {
	key := dataPrefix + path
	if rf.Object {
		var err error
		key, err = objectPath(rf.Sha)
		if err != nil {
			return nil, err
		}
	}
	b, err := e.Store.Get(key)
	if err != nil {
		return nil, err
	}
	if b == nil || (localFile{content: b}).sha() != rf.Sha {
		return nil, fmt.Errorf("sync file changed or missing: %s", path)
	}
	if rf.Unit == UnitSettings {
		return settingsBytes(b, nil)
	}
	return b, nil
}

func (e *Engine) applyFile(next, old localFile, existed bool) error {
	disk := filepath.Join(e.Root, filepath.FromSlash(next.rel))
	current, err := os.ReadFile(disk)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if existed != (err == nil) {
		return ErrConflict
	}
	projected, data := current, next.content
	if next.unit == UnitSettings {
		if current != nil {
			projected, err = settingsBytes(current, nil)
			if err != nil {
				return err
			}
		}
		data, err = settingsBytes(data, current)
		if err != nil {
			return err
		}
	}
	if !bytes.Equal(projected, old.content) {
		return ErrConflict
	}
	if err = writeAtomic(disk, data); err != nil {
		return err
	}
	e.Changed = append(e.Changed, next.rel)
	stamp := time.Unix(0, next.modTime)
	return os.Chtimes(disk, stamp, stamp)
}

// A new device writes its default settings during startup. On first contact
// those defaults should not clear a preference explicitly set elsewhere.
func initialPreferences(local, remote, defaults []byte, localNewer bool) ([]byte, error) {
	var l, r, d map[string]json.RawMessage
	for _, entry := range []struct {
		data []byte
		dst  *map[string]json.RawMessage
	}{{local, &l}, {remote, &r}, {defaults, &d}} {
		if len(entry.data) > 0 {
			if err := json.Unmarshal(entry.data, entry.dst); err != nil {
				return nil, err
			}
		}
	}
	for k, v := range l {
		other, exists := r[k]
		if !exists || bytes.Equal(other, d[k]) || localNewer && !bytes.Equal(v, d[k]) {
			r[k] = v
		}
	}
	return json.Marshal(r)
}

func objectPath(sha string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(sha)
	if err != nil || len(b) != sha1.Size {
		return "", fmt.Errorf("invalid sync digest")
	}
	return "files-v2/objects/" + hex.EncodeToString(b), nil
}

func allowedFile(rel string, unit Unit) bool {
	if !fs.ValidPath(rel) || !filepath.IsLocal(filepath.FromSlash(rel)) || strings.ContainsAny(rel, `\:`) {
		return false
	}
	parts := strings.Split(rel, "/")
	if unit == UnitSettings {
		return rel == "settings.json"
	}
	if unit != UnitPlugins || len(parts) < 3 {
		return false
	}
	for _, p := range parts {
		if strings.HasPrefix(p, ".") {
			return false
		}
	}
	return parts[0] == "plugins" || parts[0] == "plugin-data" && len(parts) == 3 && parts[2] == "granted.json"
}

// Devices can have different installed sources and update histories.
func settingsBytes(in, current []byte) ([]byte, error) {
	var shared, local map[string]json.RawMessage
	if err := json.Unmarshal(in, &shared); err != nil {
		return nil, err
	}
	if shared == nil {
		return nil, fmt.Errorf("invalid settings object")
	}
	if current != nil {
		if err := json.Unmarshal(current, &local); err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"source", "disabledPlugins", "appChecked", "pluginsChecked", "pluginUpdates"} {
		delete(shared, key)
		if v, ok := local[key]; ok {
			shared[key] = v
		}
	}
	return json.Marshal(shared)
}

func (lf localFile) sha() string {
	h := sha1.Sum(lf.content)
	return base64.StdEncoding.EncodeToString(h[:])
}

// localFiles gathers what the enabled units hold on this device.
func (e *Engine) localFiles(units map[Unit]bool) ([]localFile, error) {
	var out []localFile
	add := func(rel string, unit Unit) error {
		p := filepath.Join(e.Root, filepath.FromSlash(rel))
		info, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if unit == UnitSettings {
			b, err = settingsBytes(b, nil)
			if err != nil {
				return err
			}
		}
		out = append(out, localFile{rel: rel, unit: unit, modTime: info.ModTime().UnixNano(), content: b})
		return nil
	}
	if units[UnitSettings] {
		if err := add("settings.json", UnitSettings); err != nil {
			return nil, err
		}
	}
	if units[UnitPlugins] {
		// The plugins themselves, and what each keeps of its own setup: its
		// settings and whether programs were allowed. Not its cache.
		err := filepath.WalkDir(filepath.Join(e.Root, "plugins"), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && p != filepath.Join(e.Root, "plugins") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(e.Root, p)
			if err != nil {
				return err
			}
			return add(filepath.ToSlash(rel), UnitPlugins)
		})
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(filepath.Join(e.Root, "plugin-data"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			for _, name := range []string{"granted.json"} {
				if err := add(path.Join("plugin-data", ent.Name(), name), UnitPlugins); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}
