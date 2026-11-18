// Package profileimport copies the upstream desktop profile before Plus opens
// its settings, plugin hosts, or library. The caller owns the target app's
// single-instance lock throughout recovery and import.
package profileimport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Profiles struct {
	Source, Target, Backups string
}

func Desktop(configDir, name string) Profiles {
	return Profiles{
		filepath.Join(configDir, "MyGo Music"),
		filepath.Join(configDir, name),
		filepath.Join(configDir, name+" Migration Backups"),
	}
}

var items = []string{"settings.json", "session.json", "window-state.json", "local", "models", "plugins", "plugin-data"}

func directory(path string) error {
	info, err := os.Lstat(path)
	if err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("expected a directory: %s", path)
	}
	return err
}

func (p Profiles) Request() error {
	if err := directory(p.Source); err != nil {
		return err
	}
	if err := os.MkdirAll(p.Backups, 0700); err != nil {
		return err
	}
	if err := directory(p.Backups); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(p.Backups, "requested"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}

// Recover rolls back an interrupted directory swap before the app can create
// an empty target. A completed swap keeps its backup.
func (p Profiles) Recover() error {
	if err := directory(p.Backups); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	marker := filepath.Join(p.Backups, "desktop-pending")
	b, err := os.ReadFile(marker)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	id := string(b)
	if !strings.HasPrefix(id, "import-") || !filepath.IsLocal(id) || filepath.Base(id) != id {
		return fmt.Errorf("invalid import marker in %s", p.Backups)
	}
	job := filepath.Join(p.Backups, id)
	if err := directory(job); err != nil {
		return err
	}
	before := filepath.Join(job, "before")
	backupErr, targetErr := directory(before), directory(p.Target)
	if backupErr != nil && !errors.Is(backupErr, fs.ErrNotExist) {
		return backupErr
	}
	if targetErr != nil && !errors.Is(targetErr, fs.ErrNotExist) {
		return targetErr
	}
	if backupErr == nil {
		if errors.Is(targetErr, fs.ErrNotExist) {
			if err := os.Rename(before, p.Target); err != nil {
				return err
			}
		} else if _, err := os.Lstat(filepath.Join(job, "prepared")); !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("interrupted import has two profiles; previous profile: %s", before)
		}
	}
	return os.Remove(marker)
}

// ApplyRequested consumes one confirmation. A failure while staging leaves
// the target untouched. A successful import returns its backup directory.
func (p Profiles) ApplyRequested() (string, error) {
	if err := os.Remove(filepath.Join(p.Backups, "requested")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	for _, dir := range []string{p.Source, p.Target, p.Backups} {
		if err := directory(dir); err != nil {
			return "", err
		}
	}
	job, err := os.MkdirTemp(p.Backups, "import-")
	if err != nil {
		return "", err
	}
	stage := filepath.Join(job, "prepared")
	if err := p.stageConsistentProfile(stage); err != nil {
		_ = os.RemoveAll(job) // private directory created above, before any rename
		return "", err
	}
	marker := filepath.Join(p.Backups, "desktop-pending")
	tmp := filepath.Join(job, "pending.tmp")
	if err := os.WriteFile(tmp, []byte(filepath.Base(job)), 0600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, marker); err != nil {
		return "", err
	}
	before := filepath.Join(job, "before")
	if err := os.Rename(p.Target, before); err != nil {
		return "", err
	}
	if err := os.Rename(stage, p.Target); err != nil {
		return "", err
	}
	return before, os.Remove(marker)
}

// The upstream app owns its own lock, and can reopen during the copy. Check
// membership and timestamps again to catch edits to files already staged.
func (p Profiles) stageConsistentProfile(stage string) error {
	before, err := sourceState(p.Source)
	if err != nil {
		return err
	}
	if err := p.prepare(stage); err != nil {
		return err
	}
	after, err := sourceState(p.Source)
	if err != nil {
		return err
	}
	if !maps.Equal(before, after) {
		for name, previous := range before {
			if current, ok := after[name]; !ok || current != previous {
				return fmt.Errorf("original profile changed at %s; quit the original player and try again", name)
			}
		}
		return errors.New("files were added to the original profile during import; quit the original player and try again")
	}
	return nil
}

type stamp struct {
	size     int64
	mode     fs.FileMode
	modified time.Time
}

func sourceState(source string) (map[string]stamp, error) {
	state := map[string]stamp{}
	for _, name := range items {
		base := filepath.Join(source, name)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if path == base && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				// Directory listing timestamps may lag on Windows. The map
				// already tracks membership, including empty directories.
				state[path] = stamp{mode: fs.ModeDir}
				return nil
			}
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			state[path] = stamp{info.Size(), info.Mode(), info.ModTime()}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (p Profiles) prepare(stage string) error {
	src, err := os.ReadDir(p.Source)
	if err != nil {
		return err
	}
	incoming := map[string]bool{}
	for _, item := range src {
		if slices.Contains(items, item.Name()) {
			if item.Type()&os.ModeSymlink != 0 || item.IsDir() == strings.HasSuffix(item.Name(), ".json") {
				return fmt.Errorf("unexpected upstream entry: %s", item.Name())
			}
			incoming[item.Name()] = true
		}
	}
	if len(incoming) == 0 {
		return fmt.Errorf("no player data in %s", p.Source)
	}
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	existing, err := os.ReadDir(p.Target)
	if err != nil {
		return err
	}
	for _, item := range existing {
		name := item.Name()
		// An imported library starts a fresh merge with the cloud. Its previous
		// sync ancestor belongs to the profile saved in the backup.
		if name == "sync-history" || incoming[name] || name == "plugins" || name == "plugin-data" {
			continue
		}
		if err := copyEntry(filepath.Join(p.Target, name), filepath.Join(stage, name)); err != nil {
			return err
		}
	}
	for _, name := range items {
		if !incoming[name] || name == "plugins" || name == "plugin-data" {
			continue
		}
		dest := filepath.Join(stage, name)
		if name == "settings.json" {
			previous, err := object(filepath.Join(p.Target, name))
			if errors.Is(err, fs.ErrNotExist) {
				previous = map[string]json.RawMessage{}
			} else if err != nil {
				return err
			}
			source, err := object(filepath.Join(p.Source, name))
			if err != nil {
				return err
			}
			for key, value := range source {
				previous[key] = value
			}
			data, err := json.MarshalIndent(previous, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(dest, data, 0600); err != nil {
				return err
			}
		} else if err := copyEntry(filepath.Join(p.Source, name), dest); err != nil {
			return err
		}
	}
	return p.plugins(stage)
}

func object(path string) (map[string]json.RawMessage, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil || out == nil {
		return nil, fmt.Errorf("invalid JSON object in %s", path)
	}
	return out, nil
}

func entries(path string) ([]os.DirEntry, error) {
	if err := directory(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return os.ReadDir(path)
}

// A plugin directory may have a different name from its manifest ID. Its
// settings, grants and storage use the ID. Replace matching plugins whole.
func (p Profiles) plugins(stage string) error {
	ids := map[string]map[string]bool{}
	names := map[string]map[string]string{}
	for _, root := range []string{p.Source, p.Target} {
		ids[root], names[root] = map[string]bool{}, map[string]string{}
		list, err := entries(filepath.Join(root, "plugins"))
		if err != nil {
			return err
		}
		for _, e := range list {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			m, err := object(filepath.Join(root, "plugins", e.Name(), "plugin.json"))
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			var id string
			if json.Unmarshal(m["id"], &id) != nil || id == "" || (root == p.Source && ids[root][id]) {
				return fmt.Errorf("invalid or duplicate plugin ID in %s", e.Name())
			}
			ids[root][id], names[root][e.Name()] = true, id
		}
	}
	for _, group := range []string{"plugins", "plugin-data"} {
		copied := map[string]bool{}
		for _, root := range []string{p.Source, p.Target} {
			dir := filepath.Join(root, group)
			list, err := entries(dir)
			if err != nil {
				return err
			}
			for _, e := range list {
				n := e.Name()
				if root == p.Source && strings.HasPrefix(n, ".") {
					continue
				}
				if group == "plugins" && root == p.Target && ids[p.Source][names[root][n]] {
					continue
				}
				if group == "plugin-data" {
					if root == p.Source && ids[p.Target][n] && !ids[p.Source][n] {
						continue
					}
					if root == p.Target && (ids[p.Source][n] || copied[n]) {
						continue
					}
				}
				dest := filepath.Join(stage, group)
				if err := os.MkdirAll(dest, 0700); err != nil {
					return err
				}
				if err := copyEntry(filepath.Join(dir, n), filepath.Join(dest, n)); err != nil {
					return err
				}
				copied[n] = true
			}
		}
	}
	return nil
}

// Copy only regular files and directories, without following links or
// granting other users access. Preserve executable bits for plugin helpers.
func copyEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.Mkdir(dst, 0700); err != nil {
			return err
		}
		list, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range list {
			if err := copyEntry(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cannot copy linked or special file: %s", src)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600|info.Mode().Perm()&0100)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, in)
	err = errors.Join(err, out.Close())
	if err != nil {
		return err
	}
	after, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if n != info.Size() || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("file changed during import: %s", src)
	}
	return nil
}
