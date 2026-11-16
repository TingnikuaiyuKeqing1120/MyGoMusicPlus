package player

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mygo-music/internal/music"
)

// A save resumed from a part file asks the source for the rest with a Range
// request and lands the whole song, telling the absolute bytes as they come.
func TestSaveStreamResumes(t *testing.T) {
	audio := bytes.Repeat([]byte("song"), 16384)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "song.mp3", time.Time{}, bytes.NewReader(audio))
	}))
	defer srv.Close()
	st := &music.Stream{URL: srv.URL, Type: "mp3", Size: int64(len(audio))}
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")

	half := int64(len(audio) / 2)
	os.WriteFile(path+".part", audio[:half], 0o600)
	var last int64
	if err := SaveStream(context.Background(), st, path, half, func(n int64) { last = n }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, audio) {
		t.Errorf("resumed to %d bytes, want %d", len(b), len(audio))
	}
	if last != int64(len(audio)) {
		t.Errorf("progress ended at %d, want %d", last, len(audio))
	}
	if _, err := os.Stat(path + ".part"); err == nil {
		t.Error("left the part file beside the saved song")
	}
}

// A source that ignores the Range and sends the whole song again is saved from
// the beginning, not appended to what was kept.
func TestSaveStreamSourceIgnoresRange(t *testing.T) {
	audio := bytes.Repeat([]byte("song"), 4096)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(audio)
	}))
	defer srv.Close()
	st := &music.Stream{URL: srv.URL, Type: "mp3", Size: int64(len(audio))}
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	os.WriteFile(path+".part", audio[:100], 0o600)

	if err := SaveStream(context.Background(), st, path, 100, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, audio) {
		t.Errorf("saved %d bytes, want the whole song over the part", len(b))
	}
}

// A save cut short leaves its part as it is, to resume from.
func TestSaveStreamCutShortKeepsPart(t *testing.T) {
	audio := bytes.Repeat([]byte("song"), 16384)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "song.mp3", time.Time{}, bytes.NewReader(audio))
	}))
	defer srv.Close()
	st := &music.Stream{URL: srv.URL, Type: "mp3", Size: int64(len(audio))}
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := SaveStream(ctx, st, path, 0, nil); err == nil {
		t.Fatal("saved with a canceled context")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("left a finished file from a canceled save")
	}
	if _, err := os.Stat(path + ".part"); err != nil {
		t.Error("dropped the part a resume would carry on from")
	}
}