package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// The player bar's Downloads button opens the panel, where a song on its way
// can be canceled.
func TestDownloadsPanelCancel(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-block
	}))
	t.Cleanup(func() { srv.Close() })
	t.Cleanup(func() { close(block) })
	dir := t.TempDir()
	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	j := &dlJob{track: song("1", "宝船", "DOUDOU"), path: filepath.Join(dir, "song.mp3"), lib: a.library, quality: "high"}
	a.downloads.add(a, j)
	until(t, ch, func() bool { return j.cancel != nil })

	if err := tt.Click("Downloads"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("宝船") {
		t.Fatalf("the panel doesn't list the song: %q", tt.Texts())
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("%v; the panel shows %q", err, tt.Texts())
	}
	until(t, ch, func() bool { return j.status == dlCanceled })
	tt.Frame()
	if !tt.HasText("Canceled") {
		t.Errorf("the panel doesn't show the cancel: %q", tt.Texts())
	}
}

// A song's row marks its download on its way, and stops once it is canceled.
func TestRowDownloadState(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-block
	}))
	t.Cleanup(func() { srv.Close() })
	t.Cleanup(func() { close(block) })
	dir := t.TempDir()
	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "宝船", "DOUDOU")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	j := &dlJob{track: song("1", "宝船", "DOUDOU"), path: filepath.Join(dir, "song.mp3"), lib: a.library, quality: "high"}
	a.downloads.add(a, j)
	until(t, ch, func() bool { return j.cancel != nil })
	tt.Frame()
	if !tt.HasText("Downloading") {
		t.Fatalf("the row doesn't mark the download: %q", tt.Texts())
	}
	a.downloads.cancel(j.id)
	until(t, ch, func() bool { return j.status == dlCanceled })
	tt.Frame()
	if tt.HasText("Downloading") {
		t.Error("the row still marks a download canceled")
	}
}