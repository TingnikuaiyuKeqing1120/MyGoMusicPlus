package app

import (
	"errors"
	"slices"

	"mygo-music/internal/music"
)

// dragTracks is what a song row drags to a playlist of the sidebar: the songs
// chosen, or the one dragged.
type dragTracks []music.Track

// draggedTracks makes the value of the rows a drag takes, track i being row
// i+1 below the header.
func draggedTracks(tracks []music.Track, rows []int) dragTracks {
	var ts dragTracks
	for _, r := range rows {
		if r >= 1 && r <= len(tracks) {
			ts = append(ts, tracks[r-1])
		}
	}
	return ts
}

// reorderTracks moves the rows dragged to before row to in the listener's
// playlist pid, at once in the app and then with its source, which may refuse:
// the order goes back then.
func (a *app) reorderTracks(pid string, rows []int, to int) {
	lib := a.library
	pl := lib.playlists[pid]
	if pl == nil || !a.can("movePlaylistTrack") {
		return
	}
	before := pl.Tracks
	dragged := map[int]bool{}
	for _, r := range rows {
		if ti := r - 1; ti >= 0 && ti < len(before) {
			dragged[ti] = true
		}
	}
	if len(dragged) == 0 {
		return
	}
	ins := max(0, min(to-1, len(before)))
	var remaining, moved []music.Track
	adj := 0
	for i, tr := range before {
		if dragged[i] {
			moved = append(moved, tr)
		} else {
			remaining = append(remaining, tr)
			if i < ins {
				adj++
			}
		}
	}
	after := append(append(append([]music.Track{}, remaining[:adj]...), moved...), remaining[adj:]...)
	pl.Tracks = after
	go func() {
		err := applyMoves(lib.src, pid, before, after)
		a.post(func() {
			if err != nil {
				if lib.playlists[pid] == pl {
					pl.Tracks = before
				}
				a.actionFailed(lib, err, T("Couldn't move the song. Try again later."))
				return
			}
			a.loadMine(lib)
		})
	}()
}

// applyMoves turns the order before into after with the single moves a source
// takes, each from where a song is to where it goes.
func applyMoves(src music.Source, pid string, before, after []music.Track) error {
	cur := slices.Clone(before)
	for p := range after {
		if p < len(cur) && cur[p].ID == after[p].ID {
			continue
		}
		q := slices.IndexFunc(cur, func(t music.Track) bool { return t.ID == after[p].ID })
		if q < 0 {
			return errors.New("player: the song is gone")
		}
		tr := cur[q]
		cur = slices.Insert(slices.Delete(cur, q, q+1), p, tr)
		if err := src.MovePlaylistTrack(pid, q, p); err != nil {
			return err
		}
	}
	return nil
}