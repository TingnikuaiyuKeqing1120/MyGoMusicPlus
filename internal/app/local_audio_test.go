package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"mygo-music/internal/music"
)

// An older download with an inaccurate size hint still skips resolution and the dialog, and keeps its file index.
func TestDownloadReusesSavedFile(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	track := song("saved", "Saved song", "Artist")
	track.Source = "fake"
	path := filepath.Join(t.TempDir(), "original.flac")
	if err := os.WriteFile(path, []byte("saved audio"), 0600); err != nil {
		t.Fatal(err)
	}
	a.downloads.jobs = []*dlJob{{id: 1, track: track, path: path, size: 11000, status: dlDone}}
	a.downloads.next = 1
	a.downloads.persist()
	oldSave := saveSongAs
	t.Cleanup(func() { saveSongAs = oldSave })
	saveSongAs = func(*app, string) string { t.Error("asked where to save an existing download"); return "" }
	fake(a).stream = func(string, string) (*music.Stream, error) {
		t.Error("resolved an existing download online")
		return nil, music.ErrUnavailable
	}
	for range 2 {
		a.download(track)
		until(t, ch, func() bool { return a.downloads.active == 0 })
		if len(a.downloads.jobs) != 1 || a.downloads.jobs[0].path != path || !a.downloads.jobs[0].skipped {
			t.Fatal("saved file was duplicated or lost")
		}
		if a.downloads.jobs[0].size != 11 || !a.downloads.jobs[0].sizeVerified {
			t.Fatal("legacy size hint was not replaced by the actual file size")
		}
	}
	a.downloads.clearFinished()
	var restored dlManager
	restored.load()
	if restored.anyFinished() || len(restored.savedJobs()) != 1 || restored.localAudio(track) == nil {
		t.Fatal("clearing tasks lost the saved file or its playback mapping")
	}
}

// Missing, empty and truncated indexed files must be downloaded again instead of trusting the completed record.
func TestDownloadChecksActualSavedFile(t *testing.T) {
	for _, state := range []string{"missing", "empty", "truncated"} {
		t.Run(state, func(t *testing.T) {
			a := newTestApp()
			ch := posts(a)
			track := song("saved", "Saved song", "Artist")
			track.Source = "fake"
			dir := t.TempDir()
			path := filepath.Join(dir, songFileName(&track, "mp3"))
			if state != "missing" {
				data := []byte{}
				if state == "truncated" {
					data = []byte("part")
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			a.downloads.jobs = []*dlJob{{id: 1, track: track, path: path, size: 10, sizeVerified: true, status: dlDone}}
			a.downloads.next = 1
			a.downloads.persist()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.Write([]byte("whole song")) }))
			defer server.Close()
			fake(a).stream = func(string, string) (*music.Stream, error) { return &music.Stream{URL: server.URL, Type: "mp3"}, nil }
			a.downloads.addBatch(a, "high", []music.Track{track}, dir)
			until(t, ch, func() bool { return a.downloads.active == 0 })
			data, _ := os.ReadFile(path)
			if requests.Load() != 1 || string(data) != "whole song" || len(a.downloads.jobs) != 1 || a.downloads.jobs[0].size != 10 {
				t.Fatalf("invalid file was not replaced correctly: requests=%d size=%d", requests.Load(), len(data))
			}
		})
	}
}

// Repeated downloads share an in-flight job, even when a second batch asks for another quality and directory.
func TestDownloadDeduplicatesInFlight(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	block := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); <-block; w.Write([]byte("whole song")) }))
	defer server.Close()
	fake(a).stream = func(string, string) (*music.Stream, error) { return &music.Stream{URL: server.URL, Type: "mp3"}, nil }
	track := song("one", "One", "Artist")
	track.Source = "fake"
	a.downloads.addBatch(a, "high", []music.Track{track, track}, t.TempDir())
	a.downloads.addBatch(a, "lossless", []music.Track{track}, t.TempDir())
	if len(a.downloads.jobs) != 1 || a.downloads.active != 1 {
		t.Fatal("duplicate jobs were queued")
	}
	close(block)
	until(t, ch, func() bool { return a.downloads.active == 0 })
	if requests.Load() != 1 {
		t.Fatalf("downloaded %d copies", requests.Load())
	}
}

// A pre-existing FLAC can satisfy a download before an online source reveals its extension.
func TestDownloadFindsUnindexedFile(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	track := song("legacy", "Legacy", "Artist")
	track.Source = "fake"
	dir := t.TempDir()
	path := filepath.Join(dir, songFileName(&track, "flac"))
	if err := os.WriteFile(path, []byte("older local file"), 0600); err != nil {
		t.Fatal(err)
	}
	fake(a).stream = func(string, string) (*music.Stream, error) {
		t.Error("asked the network before checking the folder")
		return nil, music.ErrUnavailable
	}
	a.downloads.addBatch(a, "high", []music.Track{track}, dir)
	until(t, ch, func() bool { return a.downloads.active == 0 })
	if len(a.downloads.savedJobs()) != 1 || a.downloads.savedJobs()[0].path != path {
		t.Fatal("unindexed local file was not recovered")
	}
}

// Equal IDs from different sources remain separate, and colliding filenames cannot overwrite one another.
func TestDownloadSourceIdentityAndFilenameCollision(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	second := newFakeSource("other")
	addFake(a, second)
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block; w.Write([]byte(r.URL.Path)) }))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(block) })
	fake(a).stream = func(string, string) (*music.Stream, error) {
		return &music.Stream{URL: server.URL + "/first", Type: "mp3"}, nil
	}
	second.stream = func(string, string) (*music.Stream, error) {
		return &music.Stream{URL: server.URL + "/second", Type: "mp3"}, nil
	}
	first := song("same-id", strings.Repeat("Same filename ", 30), "Artist")
	first.Source = "fake"
	first.Album.Name = "Same album"
	other := first
	other.Source = "other"
	a.downloads.addBatch(a, "high", []music.Track{first, other}, t.TempDir())
	until(t, ch, func() bool { return a.downloads.jobs[0].cancel != nil && a.downloads.jobs[1].cancel != nil })
	block <- struct{}{}
	block <- struct{}{}
	until(t, ch, func() bool { return a.downloads.active == 0 })
	if len(a.downloads.jobs) != 2 || a.downloads.jobs[0].path == a.downloads.jobs[1].path {
		t.Fatal("separate songs shared a destination")
	}
	for i, want := range []string{"/first", "/second"} {
		data, _ := os.ReadFile(a.downloads.jobs[i].path)
		if string(data) != want {
			t.Fatalf("source %d saved %q", i, data)
		}
	}
}
