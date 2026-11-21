package player

import (
	"errors"
	"testing"
	"time"

	"mygo-music/internal/music"
)

type nativeFixture struct {
	commands []nativeCommand
	resolved []int
	errors   []error
}

func (f *nativeFixture) cache(string, int64) nativeCacheStatus { return nativeCacheStatus{} }

func (f *nativeFixture) command(c nativeCommand) { f.commands = append(f.commands, c) }
func (f *nativeFixture) resolve(generation, slot int, info *music.Stream, err error) {
	f.resolved = append(f.resolved, slot)
	f.errors = append(f.errors, err)
}

func nativePlayer() (*Player, *nativeFixture, chan func()) {
	posts := make(chan func(), 64)
	p := New(func(fn func()) { posts <- fn })
	f := &nativeFixture{}
	p.native = f
	p.Source = func(string) music.Source { return picky{ok: map[string]bool{"a": true, "b": true, "c": true}} }
	p.PlayQueue([]music.Track{{Source: "s", ID: "a"}, {Source: "s", ID: "b"}, {Source: "s", ID: "c"}}, 0)
	return p, f, posts
}

// Android receives the whole play order and controls, without a Go audio output.
func TestNativeQueueAndTransport(t *testing.T) {
	p, f, _ := nativePlayer()
	if len(f.commands) != 1 || len(f.commands[0].Items) != 3 || p.out != nil || p.stream != nil {
		t.Fatalf("native queue was not isolated from Go audio: %+v", f.commands)
	}
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 0, Position: 10000, Duration: 60000, Ready: true, Playing: true, WantPlay: true})
	now = now.Add(time.Second)
	if p.Position() != 11*time.Second || !p.Ready() {
		t.Fatalf("native position %v", p.Position())
	}
	p.TogglePlay()
	if last := f.commands[len(f.commands)-1]; last.Action != "play" || last.Play {
		t.Fatalf("pause: %+v", last)
	}
	p.Seek(25 * time.Second)
	if last := f.commands[len(f.commands)-1]; last.Action != "seek" || last.Position != 25000 {
		t.Fatalf("seek: %+v", last)
	}
	p.Next()
	if f.commands[len(f.commands)-1].Action != "next" {
		t.Fatal("next did not reach Android")
	}
}

// Picking the paused native track resumes it without replacing its audio or position.
func TestNativePickCurrentTrackContinues(t *testing.T) {
	p, f, _ := nativePlayer()
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 0, Position: 12000, Duration: 60000, Ready: true})
	generation, commands := p.gen, len(f.commands)
	p.PlayTrack(p.Queue(), 0)
	if last := f.commands[len(f.commands)-1]; last.Action != "play" || !last.Play || p.gen != generation || p.Position() != 12*time.Second {
		t.Fatalf("repeated pick restarted native audio: %+v", last)
	}
	p.PlayTrack(p.Queue(), 0)
	if len(f.commands) != commands+1 {
		t.Fatal("picking a playing track sent another playback command")
	}
}

// Native auto-advance changes the current track without replacing Android's queue.
func TestNativeAdvanceAndStaleEvents(t *testing.T) {
	p, f, _ := nativePlayer()
	count := len(f.commands)
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 1, Position: 1000, Ready: true, WantPlay: true, Playing: true})
	if p.Current().ID != "b" || p.at != 1 || len(f.commands) != count {
		t.Fatal("native advance restarted its queue")
	}
	p.nativeChanged(nativeState{Generation: p.gen - 1, Slot: 0, Position: 0})
	if p.Current().ID != "b" {
		t.Fatal("stale state replaced the current track")
	}
	p.ToggleShuffle()
	if p.Current().ID != "b" || f.commands[len(f.commands)-1].Action != "queue" {
		t.Fatal("shuffle lost the native current track")
	}
}

// A rejected source is resolved through the existing source API and shown once.
func TestNativeResolveErrorAndRetry(t *testing.T) {
	p, f, posts := nativePlayer()
	p.Source = func(string) music.Source { return picky{ok: map[string]bool{}} }
	p.nativeResolve(p.gen, 0)
	for len(f.resolved) == 0 {
		next(t, posts)
	}
	if !errors.Is(f.errors[0], music.ErrUnavailable) {
		t.Fatalf("source error %v", f.errors)
	}
	p.nativeDirection = stay
	s := nativeState{Generation: p.gen, Slot: 0, Error: "not available", WantPlay: true}
	p.nativeChanged(s)
	count := len(f.commands)
	p.nativeChanged(s)
	if p.Err() == "" || len(f.commands) != count {
		t.Fatal("failed native output retried on every state event")
	}
	p.PlayTrack(p.Queue(), 0)
	if p.nativeFailed || p.gen == s.Generation {
		t.Fatal("explicit retry did not reset the failed load")
	}
	// The bottom play button must also retry a failed native load.
	s.Generation = p.gen
	p.nativeChanged(s)
	p.TogglePlay()
	if p.nativeFailed || p.gen == s.Generation || f.commands[len(f.commands)-1].Action != "queue" {
		t.Fatal("play button did not retry the failed native load")
	}
}

// A source answer for a queue that was replaced cannot reach Android.
func TestNativeDropsOldResolution(t *testing.T) {
	posts := make(chan func(), 1)
	p := New(func(fn func()) { posts <- fn })
	f := &nativeFixture{}
	p.native, p.nativeActive, p.gen = f, true, 1
	p.Cue([]music.Track{{Source: "s", ID: "a"}})
	p.Source = func(string) music.Source { return picky{ok: map[string]bool{"a": true}} }
	p.nativeResolve(p.gen, 0)
	p.gen++
	next(t, posts)
	if len(f.resolved) != 0 {
		t.Fatal("old stream reached the native queue")
	}
}

// A system-media retry can fail again before readiness and must report that failure.
func TestNativeSystemRetryFailure(t *testing.T) {
	p, _, _ := nativePlayer()
	p.nativeDirection = stay
	failed := nativeState{Generation: p.gen, Slot: 0, Error: "source failed", WantPlay: true}
	p.nativeChanged(failed)
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 0, WantPlay: true})
	if p.nativeFailed || p.Err() != "" {
		t.Fatal("system retry retained the previous failure")
	}
	p.nativeChanged(failed)
	if p.Playing() || p.Err() == "" {
		t.Fatal("repeated source failure left playback active")
	}
}

// Replacing a long native queue with a shorter list must stop the old queue safely.
func TestNativeReplacesShorterQueue(t *testing.T) {
	p, f, _ := nativePlayer()
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 2, Position: 1000, Ready: true, Playing: true, WantPlay: true})
	p.PlayQueue([]music.Track{{Source: "s", ID: "a"}}, 0)
	last := f.commands[len(f.commands)-1]
	if last.Action != "queue" || len(last.Items) != 1 || p.Current().ID != "a" {
		t.Fatalf("replacement queue: %+v", last)
	}
}

// Native offline indicators follow the same quality ordering as the Go cache.
func TestNativeCacheQuality(t *testing.T) {
	p := New(func(func()) {})
	p.setNativeCache(nativeCacheStatus{Keys: []string{cacheKey("s", "a", "high")}})
	if !p.Cached("s", "a", "standard") || p.Cached("s", "a", "lossless") || p.Cached("other", "a", "standard") {
		t.Fatal("native cached songs lost their source or quality")
	}
	p.setNativeCache(nativeCacheStatus{})
	if p.Cached("s", "a", "standard") {
		t.Fatal("cleared native cache still marks the song offline")
	}
}

// Leaving a shuffled station must not reuse a resolved slot for another song.
func TestNativeEndShuffledRadio(t *testing.T) {
	p, f, _ := nativePlayer()
	p.radio = "fixture"
	p.order, p.at = []int{2, 0, 1}, 0
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 2, Position: 12000, Ready: true, WantPlay: true})
	generation := p.gen
	p.EndRadio()
	last := f.commands[len(f.commands)-1]
	if p.Current().ID != "c" || p.gen <= generation || last.Position != 12000 || len(last.Items) != 1 || last.Items[0].Track.ID != "c" {
		t.Fatalf("station compaction reused a native slot: %+v", last)
	}
}

// Previous skips an unplayable song backwards, matching the existing queue controls.
func TestNativePreviousSkipsBackwards(t *testing.T) {
	p, _, _ := nativePlayer()
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 2, Ready: true, WantPlay: true})
	p.Prev()
	p.nativeChanged(nativeState{Generation: p.gen, Slot: 1, Error: "unavailable", WantPlay: true})
	if p.Current().ID != "a" {
		t.Fatalf("previous skipped forwards to %s", p.Current().ID)
	}
}
