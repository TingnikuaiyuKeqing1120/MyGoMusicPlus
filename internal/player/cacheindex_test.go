package player

import (
	"os"
	"path/filepath"
	"testing"

	"mygo-music/internal/music"
)

// The cache knows what it keeps without reading the disk each time a view
// asks: keeping and clearing keep that knowledge up to date, and a song kept
// at a better quality counts as kept at a worse one asked for.
func TestCacheKnowsWhatItKeeps(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir, 1<<30)
	if c.Cached("fake", "1", "high") || c.Count() != 0 {
		t.Fatal("an empty cache marks a song kept")
	}

	part := filepath.Join(dir, "x.part")
	if err := os.WriteFile(part, []byte("song"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := &music.Stream{Type: "mp3", Size: 4, Quality: "high"}
	if _, ok := c.keep("fake", "1", "high", st, part, 4); !ok {
		t.Fatal("the song wasn't kept")
	}
	if !c.Cached("fake", "1", "standard") {
		t.Error("a song kept at high isn't kept at standard")
	}
	if c.Cached("fake", "1", "lossless") {
		t.Error("a song kept at high counts as kept at lossless")
	}
	if c.Count() != 1 {
		t.Errorf("count %d, want 1", c.Count())
	}

	c.Clear()
	if c.Cached("fake", "1", "high") || c.Count() != 0 {
		t.Error("clearing left a song kept")
	}

	// A song written outside the cache's own keeping is found on a refresh.
	if err := os.WriteFile(filepath.Join(dir, cacheKey("fake", "2", "high")+".mp3"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Refresh()
	if !c.Cached("fake", "2", "high") || c.Count() != 1 {
		t.Errorf("refresh missed a song on disk: count %d", c.Count())
	}
}