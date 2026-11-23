package local

import (
	"os"
	"path/filepath"
	"testing"

	"mygo-music/internal/music"
)

// Matching requires complete credits, respects versions and lengths, and rejects ambiguous or unavailable files.
func TestMatchLocalRecording(t *testing.T) {
	dir := t.TempDir()
	path := write(t, filepath.Join(dir, "song.mp3"), fixture(t, "seek.mp3"))
	s := New(t.TempDir())
	entries := []entry{{Path: path, Title: "Song", Artist: "Artist", Album: "Album", Duration: 200000}}
	s.cat.Store(buildCatalog(entries))
	track := music.Track{Name: " song ", Artists: []music.Artist{{Name: "ARTIST"}}, Album: music.Album{Name: "album"}, Duration: 201000}
	if found := s.Match(track); found == nil || found.URL != music.FileURL(path) {
		t.Fatal("complete matching credits did not resolve")
	}
	for _, change := range []func(*music.Track){
		func(t *music.Track) { t.Name = "Song (Live)" },
		func(t *music.Track) { t.Album.Name = "" },
		func(t *music.Track) { t.Artists = nil },
		func(t *music.Track) { t.Duration = 250000 },
	} {
		other := track
		change(&other)
		if s.Match(other) != nil {
			t.Fatal("incomplete or mismatched credits were accepted")
		}
	}
	second := write(t, filepath.Join(dir, "second.mp3"), fixture(t, "seek.mp3"))
	duplicate := entries[0]
	duplicate.Path = second
	s.cat.Store(buildCatalog(append(entries, duplicate)))
	if s.Match(track) != nil {
		t.Fatal("ambiguous local recordings were accepted")
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	if s.Match(track) == nil {
		t.Fatal("unavailable duplicate hid a readable recording")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if s.Match(track) != nil {
		t.Fatal("deleted file remained a playable match")
	}
}
