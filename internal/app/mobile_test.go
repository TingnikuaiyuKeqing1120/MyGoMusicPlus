package app

import (
	"fmt"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
	"mygo-music/internal/i18n"
)

// A narrow player opens its queue, changes tracks and returns through the
// visible close control, including after switching every music pane.
func TestMobilePlayerNavigation(t *testing.T) {
	for _, width := range []int{320, 390} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			language := i18n.Language()
			i18n.SetLanguage("zh-CN")
			t.Cleanup(func() { i18n.SetLanguage(language) })
			a, tt, _, _, _ := batchApp(t)
			a.pl.Cue(a.playlists["42"].Tracks)
			tt.SetSize(width, 720)
			tt.SetDark(true)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			settled(tt)
			if err := tt.Click(T("Queue")); err != nil {
				t.Fatal(err)
			}
			settled(tt)
			if !a.nowPlaying || a.npPane != paneQueue {
				t.Fatal("queue control did not open the queue")
			}
			if dir := os.Getenv("MM_FRAMES"); dir != "" {
				saveFrame(t, dir, fmt.Sprintf("now-playing-phone-%d.png", width), tt.Image())
			}
			if err := tt.Click("大海"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.pl.Current() == nil || a.pl.Current().ID != "2" {
				t.Fatal("queue tap did not change track")
			}
			for _, tab := range []struct {
				label string
				pane  int
			}{{"Lyrics", paneLyrics}, {"Stage", paneStage}, {"Wall", paneWall}, {"Up Next", paneQueue}} {
				if err := tt.Click(T(tab.label)); err != nil {
					t.Fatal(err)
				}
				settled(tt)
				if a.npPane != tab.pane {
					t.Fatalf("pane switch %s failed", tab.label)
				}
			}
			if err := tt.Click(T("Close")); err != nil {
				t.Fatal(err)
			}
			settled(tt)
			if a.nowPlaying {
				t.Fatal("close did not return to the playlist")
			}
		})
	}
}

// Scroll containers opt into panning while the player's seek bar keeps its
// pointer input. The web adapter separately filters touch and mouse devices.
func TestTouchScrollRegions(t *testing.T) {
	previous := allowTouchScroll
	t.Cleanup(func() { allowTouchScroll = previous })
	allowed := 0
	allowTouchScroll = func() { allowed++ }
	_, tt, _, _, _ := batchApp(t)
	tt.SetSize(390, 800)
	settled(tt)
	tt.UsePointer(ui.PointerTouch, 0.5)
	box, ok := tt.Find(Tf("Select %s", "宝船"))
	if !ok {
		t.Fatal("song checkbox missing")
	}
	tt.Press(box.X+box.W/2, box.Y+box.H/2)
	tt.Release(box.X+box.W/2, box.Y+box.H/2)
	if allowed != 1 {
		t.Fatalf("touch in list registered %d times", allowed)
	}
	bar, ok := tt.Find(T("Progress"))
	if !ok {
		t.Fatal("player seek bar missing")
	}
	tt.Press(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Release(bar.X+bar.W/2, bar.Y+bar.H/2)
	if allowed != 1 {
		t.Fatal("seek bar was registered as a scrolling surface")
	}
}

// Scrolling a long playlist moves its large artwork header out of the way.
// Marking it as ListState.Header used to pin it over the songs on a phone.
func TestMobilePlaylistHeaderScrollsAway(t *testing.T) {
	a, tt, _, _, _ := batchApp(t)
	for i := 4; i <= 30; i++ {
		a.playlists["42"].Tracks = append(a.playlists["42"].Tracks, song(fmt.Sprint(i), fmt.Sprintf("Song %d", i)))
	}
	tt.SetSize(390, 720)
	settled(tt)
	tt.Scroll(200, 500, 0, 700)
	settled(tt)
	if header, ok := tt.Find("飙升榜"); ok && header.Y+header.H > topBarHeight {
		t.Fatal("large playlist header stayed pinned over the scrolled songs")
	}
	if err := tt.Click(Tf("Select %s", "Song 9")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.trackSel.Has(9) || a.pl.Current() != nil {
		t.Fatal("scrolled song could not be selected independently")
	}
}
