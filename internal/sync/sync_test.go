package sync

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeStore is a repository in memory, for two devices to sync through.
type fakeStore struct {
	files map[string][]byte
	made  bool
}

func (f *fakeStore) EnsureRepo() error { f.made = true; return nil }
func (f *fakeStore) Get(p string) ([]byte, error) {
	return f.files[p], nil
}
func (f *fakeStore) Put(p string, b []byte) error {
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[p] = b
	return nil
}

func write(t *testing.T, root, rel, content string, at time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func allUnits() map[Unit]bool {
	return map[Unit]bool{UnitSettings: true, UnitPlaylists: true, UnitPlugins: true}
}

// What one device pushes, another pulls: settings and a plugin's setup.
func TestPushPullAcrossDevices(t *testing.T) {
	store := &fakeStore{}
	a, b := t.TempDir(), t.TempDir()
	now := time.Now()
	write(t, a, "settings.json", `{"quality":"lossless"}`, now)
	write(t, a, "plugins/gdstudio/plugin.json", `{"id":"gdstudio"}`, now)
	write(t, a, "plugin-data/gdstudio/settings.json", `{"source":"netease"}`, now)
	write(t, a, "plugin-data/gdstudio/storage.json", `{"covers":{}}`, now) // cache: not synced

	ea := &Engine{Root: a, Store: store}
	if err := ea.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(store.files["files-v2/plugins.json"], []byte("storage.json")) {
		t.Fatal("the plugin's cache was synced")
	}
	eb := &Engine{Root: b, Store: store}
	if err := eb.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, b, "settings.json"); got != `{"quality":"lossless"}` {
		t.Fatalf("settings didn't arrive: %s", got)
	}
	if got := read(t, b, "plugins/gdstudio/plugin.json"); got != `{"id":"gdstudio"}` {
		t.Fatalf("the plugin didn't arrive: %s", got)
	}
	if _, err := os.Stat(filepath.Join(b, "plugin-data/gdstudio/settings.json")); !os.IsNotExist(err) {
		t.Fatal("plugin settings must be applied through the live data source", err)
	}
}

// The side that wrote a file last wins; an older pull doesn't undo it.
func TestLastWriteWins(t *testing.T) {
	store := &fakeStore{}
	a, b := t.TempDir(), t.TempDir()
	t0 := time.Now().Add(-time.Hour)
	write(t, a, "settings.json", `{"v":1}`, t0)
	ea := &Engine{Root: a, Store: store}
	if err := ea.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}
	// B writes a newer version, pushes it.
	write(t, b, "settings.json", `{"v":2}`, time.Now())
	eb := &Engine{Root: b, Store: store}
	if err := eb.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}
	// A pulls: B's newer version wins.
	if err := ea.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a, "settings.json"); got != `{"v":2}` {
		t.Fatalf("the newer write didn't win: %s", got)
	}
	// A writes again, newer still; B pulls and takes it.
	write(t, a, "settings.json", `{"v":3}`, time.Now().Add(time.Hour))
	if err := ea.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}
	if err := eb.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, b, "settings.json"); got != `{"v":3}` {
		t.Fatalf("the newest write didn't win: %s", got)
	}
}

// A kind turned off doesn't sync, though another does.
func TestIndependentUnits(t *testing.T) {
	store := &fakeStore{}
	a, b := t.TempDir(), t.TempDir()
	now := time.Now()
	write(t, a, "settings.json", `{"quality":"high"}`, now)
	write(t, a, "plugins/gdstudio/plugin.json", `{"id":"gdstudio"}`, now)
	ea := &Engine{Root: a, Store: store}
	only := map[Unit]bool{UnitSettings: true}
	if err := ea.SyncFiles(only); err != nil {
		t.Fatal(err)
	}
	eb := &Engine{Root: b, Store: store}
	if err := eb.SyncFiles(allUnits()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b, "settings.json")); err != nil {
		t.Fatal("settings didn't sync")
	}
	if _, err := os.Stat(filepath.Join(b, "plugins/gdstudio/plugin.json")); !os.IsNotExist(err) {
		t.Fatal("the plugin synced though its kind was off")
	}
}

func (f *fakeStore) ReadVersion(path string) ([]byte, string, error) {
	b := f.files[path]
	if b == nil {
		return nil, "", nil
	}
	return b, (localFile{content: b}).sha(), nil
}
func (f *fakeStore) PutVersion(path string, b []byte, rev string) error {
	_, current, _ := f.ReadVersion(path)
	if current != rev {
		return fmt.Errorf("%w: %s", ErrConflict, path)
	}
	return f.Put(path, b)
}
