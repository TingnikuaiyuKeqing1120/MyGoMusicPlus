package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/i18n"
	"mygo-music/internal/music"
)

// batchApp is a playlist of three songs, signed in, with one of the listener's
// own playlists to add to, and the source's answers captured.
func batchApp(t *testing.T) (*app, *ui.Tester, chan func(), *[]string, *[]string) {
	t.Helper()
	a := newTestApp()
	ch := posts(a)
	a.library.account = &music.Account{Name: "小明"}
	a.mine = []music.Playlist{{ID: "7", Name: "开车歌单", Mine: true}, {ID: "42", Name: "飙升榜", Mine: true}}
	var added, removed []string
	fake(a).addTo = func(id string, tracks []string) error {
		added = append(added, id+":"+strings.Join(tracks, ","))
		return nil
	}
	fake(a).removeFrom = func(id string, tracks []string) error {
		removed = append(removed, id+":"+strings.Join(tracks, ","))
		return nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{
		song("1", "宝船", "DOUDOU"),
		song("2", "大海", "张雨生"),
		song("3", "晴天", "周杰伦"),
	}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	return a, tt, ch, &added, &removed
}

// Ctrl-click chooses several songs without playing them, and the batch bar
// puts them all in one of the listener's playlists at once.
func TestBatchAddToPlaylist(t *testing.T) {
	a, tt, ch, added, _ := batchApp(t)
	if err := tt.ClickWith(ui.Cmd, "宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "大海"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("2 selected") {
		t.Fatalf("the batch bar didn't show two chosen: %q", tt.Texts())
	}
	if a.pl.Current() != nil {
		t.Error("choosing a song played it")
	}
	if err := tt.Click("Add Selected to Playlist"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("开车歌单"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	until(t, ch, func() bool { return a.toast == "Added 2 songs to the playlist" })
	if len(*added) != 1 || (*added)[0] != "7:1,2" {
		t.Errorf("added %v, want both songs in playlist 7 at once", *added)
	}
}

// The batch bar takes the chosen songs out of the playlist they are listed in.
func TestBatchRemoveFromPlaylist(t *testing.T) {
	a, tt, ch, _, removed := batchApp(t)
	if err := tt.ClickWith(ui.Cmd, "宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "大海"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Remove from Playlist"); err != nil {
		t.Fatalf("%v; the bar shows %q", err, tt.Texts())
	}
	until(t, ch, func() bool { return a.toast == "Removed 2 songs from the playlist" })
	if len(*removed) != 1 || (*removed)[0] != "42:1,2" {
		t.Errorf("removed %v, want both out of playlist 42", *removed)
	}
	if len(a.playlists["42"].Tracks) != 1 {
		t.Errorf("left %d songs listed, want 1", len(a.playlists["42"].Tracks))
	}
}

// Select All chooses every song of the page, and Clear chooses none, putting
// the batch bar away.
func TestBatchSelectAllAndClear(t *testing.T) {
	_, tt, _, _, _ := batchApp(t)
	if err := tt.ClickWith(ui.Cmd, "宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "大海"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Select All"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("3 selected") {
		t.Errorf("Select All chose %q, want three", tt.Texts())
	}
	if err := tt.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("selected") {
		t.Errorf("Clear left the bar showing: %q", tt.Texts())
	}
}

// The batch bar downloads just the chosen songs into one folder.
func TestBatchDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("the song"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	real := pickFolder
	pickFolder = func(a *app, got func(string)) { got(dir) }
	defer func() { pickFolder = real }()

	a, tt, ch, _, _ := batchApp(t)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	if err := tt.ClickWith(ui.Cmd, "宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "大海"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Download"); err != nil {
		t.Fatalf("%v; the bar shows %q", err, tt.Texts())
	}
	if err := tt.ChooseMenuItem("High"); err != nil {
		t.Fatalf("%v; the menu shows %q", err, tt.Menu())
	}
	until(t, ch, func() bool { return strings.HasPrefix(a.toast, "Downloaded") })
	if a.toast != "Downloaded 2 songs" {
		t.Errorf("toast %q, want the two chosen", a.toast)
	}
	for _, name := range []string{"DOUDOU - 宝船.mp3", "张雨生 - 大海.mp3"} {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != "the song" {
			t.Errorf("%s saved %q", name, b)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "周杰伦 - 晴天.mp3")); err == nil {
		t.Error("downloaded a song that wasn't chosen")
	}
}

// A plain click still plays the song, and choosing one alone shows no bar.
func TestRowClickPlaysWithoutBar(t *testing.T) {
	a, tt, _, _, _ := batchApp(t)
	if err := tt.Click("宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if cur := a.pl.Current(); cur == nil || cur.ID != "1" {
		t.Fatalf("playing %+v, want the song clicked", cur)
	}
	if a.trackSel.Len() != 0 {
		t.Fatal("playing the row left it selected for batch actions")
	}
	if tt.HasText("selected") {
		t.Errorf("one song chosen showed the bar: %q", tt.Texts())
	}
}

// Checkboxes select without playing on desktop and phone, allow a single-item
// batch, and a plain click returns to playback without leaving a selection.
func TestTrackCheckboxSelection(t *testing.T) {
	for _, width := range []int{320, 390, 1200} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			language := i18n.Language()
			i18n.SetLanguage("zh-CN")
			t.Cleanup(func() { i18n.SetLanguage(language) })
			a, tt, ch, added, _ := batchApp(t)
			tt.SetSize(width, 800)
			tt.SetDark(true)
			settled(tt)
			if err := tt.Click(Tf("Select %s", "宝船")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.trackSel.Len() != 1 || !a.trackSel.Has(1) || a.pl.Current() != nil {
				t.Fatal("checkbox did not select exactly one song without playing")
			}
			if dir := os.Getenv("MM_FRAMES"); dir != "" {
				saveFrame(t, dir, fmt.Sprintf("playlist-checkbox-%d.png", width), tt.Image())
			}
			if err := tt.Click(T("Add Selected to Playlist")); err != nil {
				t.Fatal(err)
			}
			if err := tt.ChooseMenuItem("开车歌单"); err != nil {
				t.Fatal(err)
			}
			until(t, ch, func() bool { return a.toast == Tf("Added %d songs to the playlist", 1) })
			if len(*added) != 1 || (*added)[0] != "7:1" {
				t.Fatalf("added %v", *added)
			}
			tt.Frame()
			if err := tt.Click(Tf("Select %s", "大海")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.trackSel.Len() != 2 {
				t.Fatal("second checkbox replaced the first selection")
			}
			if err := tt.Click(T("Clear")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if err := tt.Click(Tf("Select %s", "宝船")); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if err := tt.Click("宝船"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.trackSel.Len() != 0 || a.pl.Current() == nil || a.pl.Current().ID != "1" {
				t.Fatal("plain click on a selected song failed to switch back to playback")
			}
		})
	}
}

// Batch liking adds selected songs and keeps already liked songs liked.
func TestBatchLikeCheckboxes(t *testing.T) {
	a, tt, ch, _, _ := batchApp(t)
	tt.SetSize(390, 800)
	a.liked["1"] = true
	var liked []string
	fake(a).like = func(id string, on bool) error {
		if !on {
			t.Error("batch liking unliked a song")
		}
		liked = append(liked, id)
		return nil
	}
	settled(tt)
	for _, name := range []string{"宝船", "大海"} {
		if err := tt.Click(Tf("Select %s", name)); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if err := tt.Click(T("Like Selected")); err != nil {
		t.Fatal(err)
	}
	until(t, ch, func() bool { return len(a.likedList) == 1 })
	if len(liked) != 1 || liked[0] != "2" || !a.liked["1"] || !a.liked["2"] {
		t.Fatalf("likes=%v state=%v", liked, a.liked)
	}
}

// Delete takes the chosen songs out of the playlist they are listed in.
func TestBatchDeleteKey(t *testing.T) {
	a, tt, ch, _, removed := batchApp(t)
	if err := tt.ClickWith(ui.Cmd, "宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "大海"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(0, ui.KeyDelete)
	until(t, ch, func() bool { return a.toast == "Removed 2 songs from the playlist" })
	if len(*removed) != 1 || (*removed)[0] != "42:1,2" {
		t.Errorf("removed %v, want both out of playlist 42", *removed)
	}
}
