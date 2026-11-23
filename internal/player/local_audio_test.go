package player

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mygo-music/internal/music"
)

// The audio resolver can play a downloaded copy while its original plugin is unavailable.
func TestOpenPrefersLocalAudio(t *testing.T) {
	p := New(func(func()) {})
	path, err := filepath.Abs("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	track := music.Track{ID: "online-id", Source: "online"}
	p.LocalAudio = func(got music.Track) *music.Stream {
		if got.ID != track.ID || got.Source != track.Source {
			t.Error("local resolution lost online identity")
		}
		return &music.Stream{URL: music.FileURL(path), Type: "mp3"}
	}
	info, audio, err := p.open(nil, track, "lossless", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer audio.Close()
	if info.URL != music.FileURL(path) {
		t.Fatal("local file was not used")
	}
}

// A local file removed after prefetch falls back to the source at playback time.
func TestRemovedLocalPrefetchFallsBack(t *testing.T) {
	path, err := filepath.Abs("testdata/seek.mp3")
	if err != nil {
		t.Fatal(err)
	}
	p := New(func(func()) {})
	pre := &ahead{done: make(chan struct{}), info: &music.Stream{URL: music.FileURL(filepath.Join(t.TempDir(), "removed.mp3")), Type: "mp3"}}
	close(pre.done)
	asked := 0
	source := streamer{st: music.Stream{URL: music.FileURL(path), Type: "mp3"}, asked: &asked}
	info, audio, err := p.open(source, music.Track{ID: "a", Source: "src"}, "high", pre)
	if err != nil {
		t.Fatal(err)
	}
	defer audio.Close()
	if info.URL != music.FileURL(path) || asked != 1 {
		t.Fatal("deleted prefetch did not fall back to the source")
	}
}

// Android receives local audio for an online queue slot without changing that slot's track identity.
func TestNativePrefersLocalAudio(t *testing.T) {
	p, native, posts := nativePlayer()
	p.Source = func(string) music.Source { return nil }
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	p.LocalAudio = func(track music.Track) *music.Stream {
		return &music.Stream{URL: music.FileURL(path), Type: "mp3", Size: 7}
	}
	p.nativeResolve(p.gen, 0)
	for len(native.resolved) == 0 {
		select {
		case fn := <-posts:
			fn()
		case <-time.After(2 * time.Second):
			t.Fatal("native resolution did not finish")
		}
	}
	if len(native.errors) != 1 || native.errors[0] != nil || p.info == nil || p.info.URL != music.FileURL(path) {
		t.Fatal("Android did not resolve the local audio")
	}
	if p.Current().Source != "s" || p.Current().ID != "a" {
		t.Fatal("local playback changed the online queue identity")
	}
}
