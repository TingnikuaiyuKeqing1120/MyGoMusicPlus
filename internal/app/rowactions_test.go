package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
	"mygo-music/internal/player"
)

// The add button on a song's row puts it in one of the listener's playlists,
// without playing it.
func TestRowAddToPlaylist(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	a.library.account = &music.Account{Name: "小明"}
	a.mine = []music.Playlist{{ID: "7", Name: "开车歌单", Mine: true}}
	var added []string
	fake(a).addTo = func(id string, tracks []string) error {
		added = append(added, id+":"+strings.Join(tracks, ","))
		return nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "宝船", "DOUDOU")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("Add to Playlist"); err != nil {
		t.Fatalf("%v; the row has no add button", err)
	}
	if err := tt.ChooseMenuItem("开车歌单"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	until(t, ch, func() bool { return a.toast == "Added to the playlist" })
	if len(added) != 1 || added[0] != "7:1" {
		t.Errorf("added %v, want the song in playlist 7", added)
	}
	if a.pl.Current() != nil {
		t.Error("the add button played the song")
	}
}

// A source that takes no playlists offers no add button on its rows.
func TestRowAddOnlyWhereItCan(t *testing.T) {
	a := newTestApp()
	without(a, "addToPlaylist")
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "宝船", "DOUDOU")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("Add to Playlist"); err == nil {
		t.Error("offered to add to playlists the source doesn't take")
	}
}

// A song kept in the cache is marked offline in its row, at the quality the
// settings ask for or a better one kept.
func TestRowMarksWhatIsOffline(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	a.pl.Cache = player.NewCache(dir, 1<<30)
	tr := song("1", "宝船", "DOUDOU")
	tr.Source = "fake"
	if a.isOffline(&tr) {
		t.Fatal("marked a song offline before it was kept")
	}
	sum := sha256.Sum256([]byte("fake\x00" + tr.ID))
	key := hex.EncodeToString(sum[:16]) + "-lossless"
	if err := os.WriteFile(filepath.Join(dir, key+".mp3"), []byte("the song"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.pl.Cache.Refresh()
	if !a.isOffline(&tr) {
		t.Error("a song kept at a better quality isn't marked offline")
	}
}