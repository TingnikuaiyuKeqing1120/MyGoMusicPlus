package profileimport

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func put(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func checkText(t *testing.T, root, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil || string(got) != want {
		t.Fatalf("%s: %q, %v", name, got, err)
	}
}

// A confirmed import preserves target-only preferences and plugins, backs up prior state and never inherits an obsolete plugin grant.
func TestImport(t *testing.T) {
	dir := t.TempDir()
	p := Desktop(dir, "MyGo Music Plus Dev")
	stable := Desktop(dir, "MyGo Music Plus")
	put(t, stable.Target, "settings.json", "stable")
	put(t, p.Source, "settings.json", `{"language":"zh-CN"}`)
	put(t, p.Source, "plugins/gd-music-plugin/plugin.json", `{"id":"gdstudio"}`)
	put(t, p.Source, "plugin-data/gdstudio/storage.json", `{"liked":["song"]}`)
	put(t, p.Source, "local/library.json", `{"folders":["music"]}`)
	put(t, p.Target, "settings.json", `{"language":"en","updateMirror":true}`)
	put(t, p.Target, "plugins/older-gd/plugin.json", `{"id":"gdstudio"}`)
	put(t, p.Target, "sync-history/test/local.json", `{"likes":["before"]}`)
	put(t, p.Target, "plugins/gdstudio/plugin.json", `{"id":"gdstudio"}`)
	put(t, p.Target, "plugin-data/gdstudio/granted.json", "true")
	put(t, p.Target, "plugins/extra/plugin.json", `{"id":"extra"}`)
	put(t, p.Target, "sync.json", `{"repo":"fixture"}`)
	if backup, err := p.ApplyRequested(); backup != "" || err != nil {
		t.Fatal(backup, err)
	}
	if err := p.Request(); err != nil {
		t.Fatal(err)
	}
	before, err := p.ApplyRequested()
	if err != nil {
		t.Fatal(err)
	}
	checkText(t, before, "settings.json", `{"language":"en","updateMirror":true}`)
	data, err := object(filepath.Join(p.Target, "settings.json"))
	if err != nil || string(data["language"]) != `"zh-CN"` || string(data["updateMirror"]) != "true" {
		t.Fatal(data, err)
	}
	checkText(t, p.Target, "plugins/gd-music-plugin/plugin.json", `{"id":"gdstudio"}`)
	checkText(t, p.Target, "plugin-data/gdstudio/storage.json", `{"liked":["song"]}`)
	checkText(t, p.Target, "plugins/extra/plugin.json", `{"id":"extra"}`)
	checkText(t, p.Target, "sync.json", `{"repo":"fixture"}`)
	checkText(t, p.Target, "local/library.json", `{"folders":["music"]}`)
	for _, name := range []string{"plugins/gdstudio", "plugins/older-gd", "sync-history", "plugin-data/gdstudio/granted.json"} {
		if _, err := os.Stat(filepath.Join(p.Target, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	checkText(t, before, "sync-history/test/local.json", `{"likes":["before"]}`)
	checkText(t, stable.Target, "settings.json", "stable")
	checkText(t, p.Source, "settings.json", `{"language":"zh-CN"}`)
	if backup, err := p.ApplyRequested(); backup != "" || err != nil {
		t.Fatal(backup, err)
	}
}

// A crash after the old profile was renamed must restore it before startup creates a new data directory.
func TestRecoverBeforeOpeningProfile(t *testing.T) {
	p := Desktop(t.TempDir(), "MyGo Music Plus")
	put(t, p.Backups, "import-123/before/settings.json", "{}")
	put(t, p.Backups, "desktop-pending", "import-123")
	if err := p.Recover(); err != nil {
		t.Fatal(err)
	}
	checkText(t, p.Target, "settings.json", "{}")
	put(t, p.Source, "settings.json", "{")
	if err := p.Request(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRequested(); err == nil {
		t.Fatal("accepted malformed preferences")
	}
	checkText(t, p.Target, "settings.json", "{}")
}

// Interrupted cleanup after a successful swap keeps the new profile and the backup.
func TestRecoverCompletedSwap(t *testing.T) {
	p := Desktop(t.TempDir(), "MyGo Music Plus")
	put(t, p.Target, "settings.json", "{}")
	put(t, p.Backups, "import-123/before/settings.json", "old")
	put(t, p.Backups, "desktop-pending", "import-123")
	if err := p.Recover(); err != nil {
		t.Fatal(err)
	}
	checkText(t, p.Target, "settings.json", "{}")
	checkText(t, p.Backups, "import-123/before/settings.json", "old")
}

// Desktop plugin helpers keep their execute bit, and linked data aborts the import before the old profile is replaced.
func TestUnixHelperAndLinkedData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions and symlinks")
	}
	p := Desktop(t.TempDir(), "MyGo Music Plus")
	put(t, p.Source, "plugins/example/plugin.json", `{"id":"example"}`)
	put(t, p.Source, "plugins/example/helper", "helper")
	put(t, p.Target, "settings.json", "{}")
	helper := filepath.Join("plugins", "example", "helper")
	if err := os.Chmod(filepath.Join(p.Source, helper), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(p.Target, filepath.Join(p.Source, "local")); err != nil {
		t.Fatal(err)
	}
	if err := p.Request(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRequested(); err == nil {
		t.Fatal("imported a linked library")
	}
	checkText(t, p.Target, "settings.json", "{}")
	if err := os.Remove(filepath.Join(p.Source, "local")); err != nil {
		t.Fatal(err)
	}
	if err := p.Request(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRequested(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(p.Target, helper))
	if err != nil || st.Mode().Perm()&0100 == 0 {
		t.Fatalf("missing executable helper: %v", err)
	}
}
