package sync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

type memoryCollection struct{ Collection }

func (c *memoryCollection) Snapshot() (Collection, error) { return c.Collection, nil }
func (c *memoryCollection) ApplyCollection(before, after Collection) error {
	c.Collection = MergeCollection(before, c.Collection, after)
	return nil
}

// An empty phone signing in first cannot remove desktop lists; later edits
// use the shared ancestor to propagate a real removal and concurrent additions.
func TestSyncEmptyPhoneAndDesktop(t *testing.T) {
	store := &fakeStore{}
	phoneRoot, deskRoot := t.TempDir(), t.TempDir()
	phone := &memoryCollection{}
	desktop := &memoryCollection{Collection{Likes: []string{"a"}, Playlists: []List{{ID: "p", Name: "Road", Tracks: []string{"a", "b"}}}}}
	run := func(root string, c *memoryCollection) {
		t.Helper()
		if _, err := SyncCollection(store, root, "me/music", "local", c); err != nil {
			t.Fatal(err)
		}
	}
	run(phoneRoot, phone)
	run(deskRoot, desktop)
	run(phoneRoot, phone)
	if len(phone.Playlists) != 1 || !slices.Equal(phone.Playlists[0].Tracks, []string{"a", "b"}) {
		t.Fatalf("phone: %+v", phone)
	}
	phone.Playlists[0].Tracks = []string{"a", "phone"}
	desktop.Playlists[0].Tracks = append(desktop.Playlists[0].Tracks, "desktop")
	run(deskRoot, desktop)
	run(phoneRoot, phone)
	run(deskRoot, desktop)
	for _, c := range []*memoryCollection{phone, desktop} {
		if !slices.Equal(c.Playlists[0].Tracks, []string{"a", "phone", "desktop"}) {
			t.Fatal(c)
		}
	}
	phone.Collection = Collection{}
	run(phoneRoot, phone)
	if len(phone.Playlists) != 1 || len(phone.Playlists[0].Tracks) != 3 {
		t.Fatal("empty profile with a previous sync erased the collection")
	}
}

type conflictStore struct {
	fakeStore
	onPut func()
}

func (s *conflictStore) PutVersion(path string, b []byte, rev string) error {
	if s.onPut != nil {
		f := s.onPut
		s.onPut = nil
		f()
	}
	return s.fakeStore.PutVersion(path, b, rev)
}

// A phone writing while the desktop transfers data cannot disappear from the
// remote result, and edits made locally during the transfer are also retained.
func TestSyncRevisionConflict(t *testing.T) {
	s := &conflictStore{}
	c := &memoryCollection{Collection{Likes: []string{"desk"}}}
	s.onPut = func() {
		s.Put("collections/local.json", []byte(`{"likes":["phone"]}`))
		c.Likes = append(c.Likes, "during")
	}
	if _, err := SyncCollection(s, t.TempDir(), "me/music", "local", c); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"desk", "phone", "during"} {
		if !slices.Contains(c.Likes, id) || !bytes.Contains(s.files["collections/local.json"], []byte(id)) {
			t.Fatal(c)
		}
	}
}

// The first merge keeps non-default preferences and a device's active source.
func TestFreshPhonePreferences(t *testing.T) {
	store := &fakeStore{}
	def := []byte(`{"quality":"high","language":"system"}`)
	desk := &Engine{Root: t.TempDir(), Store: store, Defaults: def}
	phone := &Engine{Root: t.TempDir(), Store: store, Defaults: def}
	write(t, desk.Root, "settings.json", `{"quality":"lossless","language":"system","source":"gdstudio"}`, time.Unix(100, 0))
	write(t, phone.Root, "settings.json", `{"quality":"high","language":"zh","source":"local"}`, time.Unix(200, 0))
	for _, e := range []*Engine{phone, desk, phone} {
		if err := e.SyncFiles(allUnits()); err != nil {
			t.Fatal(err)
		}
	}
	for e, want := range map[*Engine]string{desk: "gdstudio", phone: "local"} {
		var s map[string]string
		if err := json.Unmarshal([]byte(read(t, e.Root, "settings.json")), &s); err != nil {
			t.Fatal(err)
		}
		if s["source"] != want || s["quality"] != "lossless" || s["language"] != "zh" {
			t.Fatalf("got %v", s)
		}
	}
}

// The v0.3.0 cloud library seeds a fresh collection without copying its paths.
func TestLegacyLibraryImport(t *testing.T) {
	s := &fakeStore{}
	s.Put("data/local/library.json", []byte(`{"folders":["E:/Music"],"recent":["secret"],"playlists":[{"id":"old","name":"Saved"}]}`))
	c := &memoryCollection{}
	root := t.TempDir()
	if _, err := SyncCollection(s, root, "me/repo", "local", c); err != nil {
		t.Fatal(err)
	}
	if len(c.Playlists) != 1 || c.Playlists[0].ID != "old" || bytes.Contains(s.files["collections/local.json"], []byte("E:/Music")) {
		t.Fatal(c)
	}
	if _, err := os.Stat(filepath.Join(root, "local/library.json")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
