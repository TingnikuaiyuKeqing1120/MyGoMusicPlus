package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"mygo-music/internal/music"
)

// A download on its way can be canceled, leaving no file behind.
func TestDownloadCancel(t *testing.T) {
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
	path := filepath.Join(dir, "song.mp3")
	j := &dlJob{track: song("1", "宝船", "DOUDOU"), path: path, lib: a.library, quality: "high"}
	a.downloads.add(a, j)
	until(t, ch, func() bool { return j.cancel != nil })
	a.downloads.cancel(j.id)
	until(t, ch, func() bool { return j.status == dlCanceled })
	if _, err := os.Stat(path); err == nil {
		t.Error("a canceled download left its file")
	}
}

// A download that failed can be retried, and lands when its source answers.
func TestDownloadRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "no", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("the song"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	path := filepath.Join(dir, "song.mp3")
	j := &dlJob{track: song("1", "宝船", "DOUDOU"), path: path, lib: a.library, quality: "high"}
	a.downloads.add(a, j)
	until(t, ch, func() bool { return j.status == dlFailed })
	a.downloads.retry(a, j.id)
	until(t, ch, func() bool { return j.status == dlDone })
	if b, _ := os.ReadFile(path); string(b) != "the song" {
		t.Errorf("saved %q after retry", b)
	}
}

// More songs than download at once all land, the queue draining as they go.
func TestDownloadQueueDrains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("the song"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	var jobs []*dlJob
	for _, id := range []string{"1", "2", "3", "4", "5"} {
		j := &dlJob{track: song(id, "歌"+id, "DOUDOU"), path: filepath.Join(dir, id+".mp3"), lib: a.library, quality: "high"}
		jobs = append(jobs, j)
		a.downloads.add(a, j)
	}
	until(t, ch, func() bool {
		for _, j := range jobs {
			if j.status != dlDone {
				return false
			}
		}
		return true
	})
	for _, j := range jobs {
		if b, _ := os.ReadFile(j.path); string(b) != "the song" {
			t.Errorf("%s saved %q", j.path, b)
		}
	}
}

// What was saved is kept on disk and shows again as jobs done after a restart.
func TestDownloadPersist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("the song"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3"}, nil
	}
	j := &dlJob{track: song("1", "宝船", "DOUDOU"), path: filepath.Join(dir, "song.mp3"), lib: a.library, quality: "high"}
	a.downloads.add(a, j)
	until(t, ch, func() bool { return j.status == dlDone })

	var reloaded dlManager
	reloaded.load()
	if len(reloaded.jobs) == 0 {
		t.Fatal("nothing saved shows after a restart")
	}
	last := reloaded.jobs[len(reloaded.jobs)-1]
	if last.status != dlDone || last.track.ID != "1" || last.path != j.path {
		t.Errorf("reloaded %+v", last)
	}
}