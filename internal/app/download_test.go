package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// Download All saves each song of a playlist into one folder picked once,
// naming them by their credits, and a song listed twice is saved once.
func TestDownloadAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("the song"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	real := pickFolder
	pickFolder = func(a *app, got func(string)) { got(dir) }
	defer func() { pickFolder = real }()

	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{
		song("1", "Back in Black", "AC/DC"),
		song("1", "Back in Black", "AC/DC"),
		song("2", "Hells Bells", "AC/DC"),
	}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("Download All"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	until(t, ch, func() bool { return strings.HasPrefix(a.toast, "Downloaded") })
	if a.toast != "Downloaded 2 songs" {
		t.Errorf("toast %q, want two songs saved once each", a.toast)
	}
	for _, name := range []string{"AC_DC - Back in Black.mp3", "AC_DC - Hells Bells.mp3"} {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); string(b) != "the song" {
			t.Errorf("%s saved %q", name, b)
		}
	}
}

// A song already in the folder is left as it is, not downloaded again.
func TestDownloadAllSkipsWhatIsThere(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("downloaded again"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "DOUDOU - 宝船.mp3"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	real := pickFolder
	pickFolder = func(a *app, got func(string)) { got(dir) }
	defer func() { pickFolder = real }()

	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "宝船", "DOUDOU")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("Download All"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	until(t, ch, func() bool { return strings.HasPrefix(a.toast, "Downloaded") })
	if b, _ := os.ReadFile(filepath.Join(dir, "DOUDOU - 宝船.mp3")); string(b) != "kept" {
		t.Errorf("overwrote a song already there: %q", b)
	}
}

// A playlist of songs that play elsewhere offers no Download All.
func TestDownloadAllOnlyWhatStreams(t *testing.T) {
	a := newTestApp()
	fake(a).info.Playback = music.PlaybackRemote
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "宝船", "DOUDOU")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	if err := tt.Click("Download All"); err == nil {
		t.Error("offered to download songs that play elsewhere")
	}
}