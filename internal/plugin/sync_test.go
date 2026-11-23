package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	cloud "mygo-music/internal/sync"
)

type dataStore map[string][]byte

func (s dataStore) ReadVersion(path string) ([]byte, string, error) {
	b := s[path]
	return b, string(b), nil
}
func (s dataStore) PutVersion(path string, b []byte, rev string) error {
	if string(s[path]) != rev {
		return cloud.ErrConflict
	}
	s[path] = bytes.Clone(b)
	return nil
}

// A plugin with an unfamiliar ID and storage layout syncs through the same
// entry point as every other plugin; only declared cache keys stay per-device.
func TestGenericPluginDataAcrossDevices(t *testing.T) {
	makePlugin := func(root, side string) *Plugin {
		t.Helper()
		dir := filepath.Join(root, "code")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		manifest := `{"id":"future-radio","name":"Future","sync":{"localKeys":["artworkCache"],"localSettings":["service"]}}`
		source := `let saved=host.storage.get("shelves")||[];
export function playlists(){return saved.map(s=>({id:s.id,name:s.name,mine:true}));}
export function createPlaylist({name}){const items=host.storage.get("shelves")||[];const p={id:name,name};items.push(p);host.storage.set("shelves",items);saved=items;return p;}
export function suggest(){return [host.setting("server")||"empty"];}`
		for name, data := range map[string]string{"plugin.json": manifest, "index.js": source} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		data := filepath.Join(root, "data", "future-radio")
		if err := os.MkdirAll(data, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(data, "storage.json"), []byte(fmt.Sprintf(`{"artworkCache":%q}`, side)), 0600); err != nil {
			t.Fatal(err)
		}
		p, err := Load(dir, filepath.Dir(data))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	leftRoot, rightRoot := t.TempDir(), t.TempDir()
	left, right := makePlugin(leftRoot, "desktop"), makePlugin(rightRoot, "phone")
	changes := 0
	left.OnDataChange(func() { changes++ })
	if err := left.SetSetting("service", "domestic"); err != nil {
		t.Fatal(err)
	}
	if err := right.SetSetting("service", "international"); err != nil {
		t.Fatal(err)
	}
	if _, err := left.CreatePlaylist("first"); err != nil {
		t.Fatal(err)
	}
	if err := left.SetSetting("server", "https://example.test"); err != nil {
		t.Fatal(err)
	}
	if changes != 2 {
		t.Fatal("user writes did not notify sync", changes)
	}
	if lists, err := right.Playlists(); err != nil || len(lists) != 0 {
		t.Fatal(lists, err)
	}
	store := dataStore{}
	run := func(root string, p *Plugin) {
		t.Helper()
		if _, err := cloud.SyncDocument(store, root, "me/music", "plugin-future-radio", p); err != nil {
			t.Fatal(err)
		}
	}
	run(rightRoot, right)
	run(leftRoot, left)
	run(rightRoot, right)
	for _, p := range []*Plugin{left, right} {
		lists, err := p.Playlists()
		if err != nil || len(lists) != 1 || lists[0].Name != "first" {
			t.Fatal(lists, err)
		}
		if p.Setting("server") != "https://example.test" {
			t.Fatal("setting missing")
		}
	}
	if bytes.Contains(store["documents/plugin-future-radio.json"], []byte("artworkCache")) {
		t.Fatal("cache uploaded")
	}
	for p, side := range map[*Plugin]string{left: "desktop", right: "phone"} {
		if string(p.storage["artworkCache"]) != fmt.Sprintf("%q", side) {
			t.Fatal("device cache changed")
		}
	}
	if left.Setting("service") != "domestic" || right.Setting("service") != "international" {
		t.Fatal("device-specific service overwritten by sync")
	}
	if bytes.Contains(store["documents/plugin-future-radio.json"], []byte("service")) {
		t.Fatal("device-specific setting uploaded")
	}
	filtered, err := left.FilterData(cloud.Document{"settings": json.RawMessage(`{"service":"old-cloud-value","server":"keep"}`)})
	if err != nil || bytes.Contains(filtered["settings"], []byte("service")) || !bytes.Contains(filtered["settings"], []byte("keep")) {
		t.Fatal("old cloud settings were not filtered", filtered, err)
	}
	if _, err := right.CreatePlaylist("phone-new"); err != nil {
		t.Fatal(err)
	}
	run(rightRoot, right)
	run(leftRoot, left)
	reopened, err := Load(left.dir, filepath.Dir(left.data))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	lists, err := reopened.Playlists()
	if err != nil || len(lists) != 2 {
		t.Fatal(lists, err)
	}
	var stored map[string]any
	data, err := os.ReadFile(filepath.Join(left.data, "storage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["artworkCache"] != "desktop" {
		t.Fatal(stored)
	}
}
