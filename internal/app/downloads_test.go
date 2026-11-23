package app

import (
	"bytes"
	"fmt"
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

// A download paused keeps what it has saved, and resumed carries on from there
// with a Range request, landing the whole song without asking for the start again.
func TestDownloadPauseResume(t *testing.T) {
	half := int64(128 << 10)
	audio := bytes.Repeat([]byte("song"), int(half*2)/4)
	block := make(chan struct{})
	var ranged atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.WriteHeader(http.StatusOK)
			w.Write(audio[:half])
			w.(http.Flusher).Flush()
			<-block
			return
		}
		ranged.Add(1)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", half, len(audio)-1, len(audio)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(audio[half:])
	}))
	t.Cleanup(func() { srv.Close() })
	t.Cleanup(func() { close(block) })
	dir := t.TempDir()
	a := newTestApp()
	ch := posts(a)
	fake(a).stream = func(id, quality string) (*music.Stream, error) {
		return &music.Stream{URL: srv.URL, Type: "mp3", Size: int64(len(audio))}, nil
	}
	path := filepath.Join(dir, "song.mp3")
	oldSave := saveSongAs
	t.Cleanup(func() { saveSongAs = oldSave })
	var dialogs atomic.Int32
	saveSongAs = func(*app, string) string { dialogs.Add(1); return path }
	j := &dlJob{track: song("1", "宝船", "DOUDOU"), choosePath: true, lib: a.library, quality: "high"}
	a.downloads.add(a, j)
	until(t, ch, func() bool { return j.done >= half })
	a.downloads.pause(j.id)
	until(t, ch, func() bool { return j.status == dlPaused })
	if info, err := os.Stat(path + ".part"); err != nil || info.Size() != half {
		t.Errorf("pause left part %v (err %v), want the first %d bytes kept", info, err, half)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a paused download left a finished file")
	}
	a.downloads.resume(a, j.id)
	until(t, ch, func() bool { return j.status == dlDone })
	if ranged.Load() != 1 {
		t.Errorf("asked for the rest %d times, want one Range request", ranged.Load())
	}
	if dialogs.Load() != 1 {
		t.Errorf("pause/resume reopened the save dialog: %d calls", dialogs.Load())
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, audio) {
		t.Errorf("resumed to %d bytes, want the whole %d", len(b), len(audio))
	}
}
