package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"mygo-music/internal/local"
	"mygo-music/internal/plugin"
	"mygo-music/internal/sync"
)

// The token and choices stay on this device, and come back as they were kept.
func TestSyncConfigRoundTrip(t *testing.T) {
	c := sync.Config{Token: "ghp_secret", Repo: "my-sync", Units: map[sync.Unit]bool{sync.UnitSettings: true}}
	if err := writeSyncConfig(c); err != nil {
		t.Fatal(err)
	}
	got := loadSyncConfig()
	if got.Token != "ghp_secret" || got.Repo != "my-sync" || !got.Units[sync.UnitSettings] {
		t.Fatalf("the config didn't come back: %+v", got)
	}
	if !got.Enabled() {
		t.Fatal("a token, a repo and a kind on should enable syncing")
	}
	// sync.json, which holds the token, sits outside every unit's paths, so
	// the engine never gathers it.
}

// Only GD songs referenced by a like or list cross devices, and the already
// running phone plugin can display and edit those songs without restarting.
func TestGDLiveCollection(t *testing.T) {
	makePlayer := func(root, storage string) *plugin.Plugin {
		t.Helper()
		if storage != "" {
			dir := filepath.Join(root, bundledGDID)
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "storage.json"), []byte(storage), 0600); err != nil {
				t.Fatal(err)
			}
		}
		p, err := plugin.Load(filepath.Join("bundled", bundledGDID), root)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	desktop := makePlayer(t.TempDir(), `{"playlists":[{"id":"p","name":"Music","mine":true,"trackIds":["a"]}],"liked":["a"],"songs":{"a":{"t":{"id":"a","name":"A","album":{}}},"private-search":{"t":{}}},"token":"desktop-private"}`)
	root := t.TempDir()
	phone := makePlayer(root, `{"history":["phone-history"],"songs":{"cached":{}}}`)
	pc, mobile := plugin.GDCollection{Player: desktop}, plugin.GDCollection{Player: phone}
	want, err := pc.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("private")) {
		t.Fatalf("private plugin data in collection: %s", b)
	}
	empty, err := mobile.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err = mobile.ApplyCollection(empty, want); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*plugin.Plugin{phone, makePlayer(root, "")} {
		list, err := p.Playlist("p")
		if err != nil || len(list.Tracks) != 1 || list.Tracks[0].Name != "A" {
			t.Fatalf("playlist=%+v err=%v", list, err)
		}
		liked, err := p.Liked(0)
		if err != nil || len(liked.Tracks) != 1 {
			t.Fatalf("liked=%+v err=%v", liked, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, bundledGDID, "storage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("phone-history")) || !bytes.Contains(data, []byte("cached")) {
		t.Fatal("lost local storage")
	}
}

// Applying cloud lists to a live local source preserves its folder and an edit
// made after the snapshot, including the next write of the in-memory library.
func TestLocalCollectionLiveEdit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "library.json"), []byte(`{"folders":["own"],"recent":["r"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	source := local.New(root)
	before, _ := source.Snapshot()
	created, err := source.CreatePlaylist("During sync")
	if err != nil {
		t.Fatal(err)
	}
	if err = source.ApplyCollection(before, sync.Collection{Playlists: []sync.List{{ID: "remote", Name: "Remote"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = source.CreatePlaylist("After sync"); err != nil {
		t.Fatal(err)
	}
	reopened := local.New(root)
	for _, s := range []*local.Source{source, reopened} {
		got, _ := s.Snapshot()
		if len(got.Playlists) != 3 || !slices.ContainsFunc(got.Playlists, func(l sync.List) bool { return l.ID == created.ID }) || !slices.Equal(s.Folders(), []string{"own"}) {
			t.Fatal(got, s.Folders())
		}
	}
}

// A tap on the playlist switch saves the actual new value on a phone layout.
func TestPhonePlaylistSyncSwitch(t *testing.T) {
	wasWeb := onWeb
	onWeb = true
	t.Cleanup(func() { onWeb = wasWeb })
	a := newTestApp()
	a.settingsWeb, a.settingsPage = true, "sync"
	a.syncer.cfg.Units = map[sync.Unit]bool{}
	tt := ui.NewTester(a.view, 360, 1000)
	settled(tt)
	for _, want := range []bool{true, false} {
		clickSettingRole(t, tt, ui.RoleSwitch, T("Sync Playlists"))
		tt.Frame()
		if a.syncer.cfg.Units[sync.UnitPlaylists] != want || loadSyncConfig().Units[sync.UnitPlaylists] != want {
			t.Fatalf("wanted switch %v", want)
		}
	}
}

// While sync is active the phone shows the real processed fraction and blocks a second run.
func TestPhoneSyncProgress(t *testing.T) {
	wasWeb := onWeb
	onWeb = true
	t.Cleanup(func() { onWeb = wasWeb })
	a := newTestApp()
	a.settingsWeb, a.settingsPage = true, "sync"
	a.syncer.cfg = sync.Config{Token: "fixture", Repo: "fixture", Units: map[sync.Unit]bool{sync.UnitPlaylists: true}}
	a.syncer.busy = true
	a.syncer.phase = "Merging playlists and likes"
	a.syncer.done, a.syncer.total = 1, 2
	tt := ui.NewTester(a.view, 360, 1100)
	settled(tt)
	n, ok := tt.AccessByRole(ui.RoleProgress, syncStatusDesc(&a.syncer))
	if !ok || n.Now != 0.5 {
		t.Fatalf("sync progress: %+v", n)
	}
	button, ok := tt.AccessByRole(ui.RoleButton, T("Sync Now"))
	if !ok || !button.Disabled {
		t.Fatal("sync button must wait for active transfer")
	}
	a.endSync(nil)
	tt.Frame()
	button, _ = tt.AccessByRole(ui.RoleButton, T("Sync Now"))
	if button.Disabled {
		t.Fatal("cannot start next sync")
	}
}

// Nothing syncs without a token, a repo, or a kind turned on.
func TestSyncEnabledGating(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	a.syncer.cfg = sync.Config{Repo: "r", Units: map[sync.Unit]bool{sync.UnitSettings: true}} // no token
	a.syncNow()
	if a.syncer.busy {
		t.Fatal("syncing started without a token")
	}
	a.markSyncDirty()
	if a.syncer.dirty {
		t.Fatal("a change was marked while syncing is off")
	}
	select {
	case fn := <-ch:
		fn()
		t.Fatal("something was posted while syncing is off")
	default:
	}
}
