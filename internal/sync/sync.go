// Package sync keeps the listener's data the same across their devices,
// through a private GitHub repository: what changed here is pushed, what
// changed elsewhere is pulled, each file taken from whichever side wrote it
// last. On pull, local-library playlists merge by ID and likes are combined.
package sync

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
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
	Token string        `json:"token"`
	Repo  string        `json:"repo"`
	Units map[Unit]bool `json:"units"`
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
	Root  string // the user data directory
	Store Store
}

// localFile is a file on this device, offered to sync.
type localFile struct {
	rel     string // slash-separated, relative to Root
	unit    Unit
	modTime int64
	content []byte
}

// Push sends what changed here since the repository last heard of it, and
// updates the manifest. What's newer elsewhere is left for Pull.
func (e *Engine) Push(units map[Unit]bool) error {
	if err := e.Store.EnsureRepo(); err != nil {
		return err
	}
	m, err := e.readManifest()
	if err != nil {
		return err
	}
	locals, err := e.localFiles(units)
	if err != nil {
		return err
	}
	changed := false
	for _, lf := range locals {
		rp := dataPrefix + lf.rel
		old := m.Files[lf.rel]
		if old.Sha == lf.sha() && old.UpdatedAt >= lf.modTime {
			continue
		}
		if err := e.Store.Put(rp, lf.content); err != nil {
			return err
		}
		m.Files[lf.rel] = File{Path: lf.rel, Sha: lf.sha(), UpdatedAt: lf.modTime, Unit: lf.unit}
		changed = true
	}
	if changed {
		return e.writeManifest(m)
	}
	return nil
}

// Pull takes what changed elsewhere since this device last wrote it, writing
// it here. Playlists merge; the rest is taken whole from the newer side.
func (e *Engine) Pull(units map[Unit]bool) error {
	m, err := e.readManifest()
	if err != nil {
		return err
	}
	locals, err := e.localFiles(units)
	if err != nil {
		return err
	}
	byPath := map[string]localFile{}
	for _, lf := range locals {
		byPath[lf.rel] = lf
	}
	// Newest first, so a merge sees the local side as it is now.
	rels := make([]string, 0, len(m.Files))
	for rel := range m.Files {
		rels = append(rels, rel)
	}
	slices.Sort(rels)
	for _, rel := range rels {
		rf := m.Files[rel]
		if !units[rf.Unit] {
			continue
		}
		lf, has := byPath[rel]
		if has && lf.modTime >= rf.UpdatedAt {
			continue // this side wrote it last
		}
		b, err := e.Store.Get(dataPrefix + rel)
		if err != nil {
			return err
		}
		if b == nil {
			continue
		}
		if rf.Unit == UnitPlaylists && has {
			b = mergeLibrary(lf.content, b, rf.UpdatedAt > lf.modTime)
		}
		if err := e.writeLocal(rel, b, rf.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) readManifest() (Manifest, error) {
	b, err := e.Store.Get(manifestPath)
	if err != nil {
		return Manifest{}, err
	}
	m := Manifest{Version: 1, Files: map[string]File{}}
	if b != nil {
		if err := json.Unmarshal(b, &m); err != nil {
			return Manifest{}, fmt.Errorf("reading the sync manifest: %w", err)
		}
	}
	if m.Files == nil {
		m.Files = map[string]File{}
	}
	return m, nil
}

func (e *Engine) writeManifest(m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return e.Store.Put(manifestPath, b)
}

func (e *Engine) writeLocal(rel string, b []byte, updatedAt int64) error {
	p := filepath.Join(e.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return err
	}
	t := time.Unix(0, updatedAt)
	return os.Chtimes(p, t, t)
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
		out = append(out, localFile{rel: rel, unit: unit, modTime: info.ModTime().UnixNano(), content: b})
		return nil
	}
	if units[UnitSettings] {
		if err := add("settings.json", UnitSettings); err != nil {
			return nil, err
		}
	}
	if units[UnitPlaylists] {
		if err := add("local/library.json", UnitPlaylists); err != nil {
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
			for _, name := range []string{"settings.json", "granted.json"} {
				if err := add(path.Join("plugin-data", ent.Name(), name), UnitPlugins); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}
