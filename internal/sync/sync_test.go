package sync

import (
	"encoding/json"
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
	if err := ea.Push(allUnits()); err != nil {
		t.Fatal(err)
	}
	if !store.made {
		t.Fatal("the repository wasn't made")
	}
	if _, ok := store.files["data/plugin-data/gdstudio/storage.json"]; ok {
		t.Fatal("the plugin's cache was synced")
	}
	eb := &Engine{Root: b, Store: store}
	if err := eb.Pull(allUnits()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, b, "settings.json"); got != `{"quality":"lossless"}` {
		t.Fatalf("settings didn't arrive: %s", got)
	}
	if got := read(t, b, "plugins/gdstudio/plugin.json"); got != `{"id":"gdstudio"}` {
		t.Fatalf("the plugin didn't arrive: %s", got)
	}
	if got := read(t, b, "plugin-data/gdstudio/settings.json"); got != `{"source":"netease"}` {
		t.Fatalf("the plugin's setup didn't arrive: %s", got)
	}
}

// The side that wrote a file last wins; an older pull doesn't undo it.
func TestLastWriteWins(t *testing.T) {
	store := &fakeStore{}
	a, b := t.TempDir(), t.TempDir()
	t0 := time.Now().Add(-time.Hour)
	write(t, a, "settings.json", `{"v":1}`, t0)
	ea := &Engine{Root: a, Store: store}
	if err := ea.Push(allUnits()); err != nil {
		t.Fatal(err)
	}
	// B writes a newer version, pushes it.
	write(t, b, "settings.json", `{"v":2}`, time.Now())
	eb := &Engine{Root: b, Store: store}
	if err := eb.Push(allUnits()); err != nil {
		t.Fatal(err)
	}
	// A pulls: B's newer version wins.
	if err := ea.Pull(allUnits()); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a, "settings.json"); got != `{"v":2}` {
		t.Fatalf("the newer write didn't win: %s", got)
	}
	// A writes again, newer still; B pulls and takes it.
	write(t, a, "settings.json", `{"v":3}`, time.Now().Add(time.Hour))
	if err := ea.Push(allUnits()); err != nil {
		t.Fatal(err)
	}
	if err := eb.Pull(allUnits()); err != nil {
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
	if err := ea.Push(only); err != nil {
		t.Fatal(err)
	}
	eb := &Engine{Root: b, Store: store}
	if err := eb.Pull(allUnits()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b, "settings.json")); err != nil {
		t.Fatal("settings didn't sync")
	}
	if _, err := os.Stat(filepath.Join(b, "plugins/gdstudio/plugin.json")); !os.IsNotExist(err) {
		t.Fatal("the plugin synced though its kind was off")
	}
}

// Playlists merge by ID, the newer side winning a conflict, each device
// keeping its own folders; likes union.
func TestPlaylistMerge(t *testing.T) {
	local := `{"folders":["E:\\Music"],"likes":["x","y"],"playlists":[{"id":"1","name":"Mine","tracks":["a"]},{"id":"2","name":"Local","tracks":["b"]}],"recent":["z"]}`
	remote := `{"folders":["/phone/music"],"likes":["y","w"],"playlists":[{"id":"1","name":"Renamed","tracks":["a","c"]},{"id":"3","name":"Remote","tracks":["d"]}],"recent":["q"]}`
	got := mergeLibrary([]byte(local), []byte(remote), true)
	var l libraryJSON
	if err := json.Unmarshal(got, &l); err != nil {
		t.Fatal(err)
	}
	if len(l.Folders) != 1 || l.Folders[0] != "E:\\Music" {
		t.Fatalf("local folders weren't kept: %v", l.Folders)
	}
	if len(l.Recent) != 1 || l.Recent[0] != "z" {
		t.Fatalf("local recent weren't kept: %v", l.Recent)
	}
	byID := map[string]playlistJSON{}
	for _, p := range l.Playlists {
		byID[p.ID] = p
	}
	if len(byID) != 3 {
		t.Fatalf("playlists didn't union: %v", l.Playlists)
	}
	if byID["1"].Name != "Renamed" || len(byID["1"].Tracks) != 2 {
		t.Fatalf("the newer side didn't win the conflict: %v", byID["1"])
	}
	if _, ok := byID["2"]; !ok {
		t.Fatal("the local-only playlist was lost")
	}
	if _, ok := byID["3"]; !ok {
		t.Fatal("the remote-only playlist was lost")
	}
	if len(l.Likes) != 3 {
		t.Fatalf("likes didn't union: %v", l.Likes)
	}
	// The older remote doesn't win a conflict.
	got = mergeLibrary([]byte(local), []byte(remote), false)
	byID = map[string]playlistJSON{}
	json.Unmarshal(got, &l)
	for _, p := range l.Playlists {
		byID[p.ID] = p
	}
	if byID["1"].Name != "Mine" {
		t.Fatalf("the older side won the conflict: %v", byID["1"])
	}
}