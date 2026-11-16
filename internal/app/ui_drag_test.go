package app

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// dragTo presses the middle of from and drags to (x, y), as a pointer does.
func dragTo(tt *ui.Tester, from ui.Rect, x, y float32) {
	sx, sy := from.X+from.W/2, from.Y+from.H/2
	tt.Press(sx, sy)
	for i := 1; i <= 4; i++ {
		f := float32(i) / 4
		tt.Move(sx+(x-sx)*f, sy+(y-sy)*f)
		tt.Frame()
	}
	tt.Release(x, y)
	tt.Frame()
}

// Dragging a row in the listener's own playlist reorders it, and the source is
// told the moves.
func TestDragReorder(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	a.mine = []music.Playlist{{ID: "42", Name: "飙升榜", Mine: true}}
	var moves []string
	fake(a).move = func(id string, from, to int) error {
		moves = append(moves, fmt.Sprintf("%d>%d", from, to))
		return nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{
		song("1", "宝船", "DOUDOU"), song("2", "大海", "张雨生"), song("3", "晴天", "周杰伦"),
	}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	src, ok := tt.Find("宝船")
	if !ok {
		t.Fatal("no row to drag")
	}
	dst, _ := tt.Find("晴天")
	dragTo(tt, src, dst.X+dst.W/2, dst.Y+2)

	var got []string
	for _, tr := range a.playlists["42"].Tracks {
		got = append(got, tr.Name)
	}
	if want := []string{"大海", "宝船", "晴天"}; !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
	until(t, ch, func() bool { return len(moves) > 0 })
	if a.pl.Current() != nil {
		t.Error("dragging a row played it")
	}
}

// Dragging a song from a playlist that isn't the listener's onto one of theirs
// in the sidebar adds it there.
func TestDragToPlaylist(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	a.library.account = &music.Account{Name: "小明"}
	a.mine = []music.Playlist{{ID: "7", Name: "开车歌单", Mine: true}}
	var added []string
	fake(a).addTo = func(id string, tracks []string) error {
		added = append(added, id+":"+tracks[0])
		return nil
	}
	a.playlists["42"] = &music.Playlist{ID: "42", Name: "飙升榜", Tracks: []music.Track{song("1", "宝船", "DOUDOU")}}
	a.route = route{page: pagePlaylist, src: "fake", id: "42"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()

	src, ok := tt.Find("宝船")
	if !ok {
		t.Fatal("no row to drag")
	}
	dst, ok := tt.Find("开车歌单")
	if !ok {
		t.Fatal("no sidebar playlist to drop on")
	}
	dragTo(tt, src, dst.X+dst.W/2, dst.Y+dst.H/2)
	until(t, ch, func() bool { return a.toast == "Added 1 songs to the playlist" })
	if len(added) != 1 || added[0] != "7:1" {
		t.Errorf("added %v, want the song in playlist 7", added)
	}
}