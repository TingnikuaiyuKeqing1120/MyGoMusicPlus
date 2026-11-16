package app

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// selectedTracks are the songs chosen in the page's list, in its order, each
// once: what the batch bar acts on. The list chooses by row, track j being row
// j+1 below the header.
func (a *app) selectedTracks() []music.Track {
	var out []music.Track
	seen := map[string]bool{}
	for j := range a.pageTracks {
		tr := &a.pageTracks[j]
		if a.trackSel.Has(j+1) && !seen[tr.ID] {
			seen[tr.ID] = true
			out = append(out, *tr)
		}
	}
	return out
}

// batchBar floats over the page when several songs are chosen: a count, and
// what can be done with them all at once, as far as the source can.
func (a *app) batchBar(c *ui.Context) {
	if a.nowPlaying || a.trackSel.Len() < 2 {
		return
	}
	sel := a.selectedTracks()
	if len(sel) < 2 {
		return
	}
	p := a.pal
	ui.Box(c.Key("batch-bar")).Absolute().Left(0).Right(0).Bottom(playerBarHeight + 16).Center().PassThrough().Children(func() {
		ui.Row(c).Gap(8).Padding(8, 12).Radius(14).AlignItems(ui.Center).
			Background(p.bg).Border(1, p.hairline).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.25)).
			Children(func() {
				ui.Text(c, Tf("%d selected", len(sel))).FontSize(13).FontWeight(600).Margin(0, 4, 0, 0)
				if a.can("addToPlaylist") && !a.library.signedOut() {
					b := pillButton(c.Key("batch-add"), iconPlus, T("Add Selected to Playlist"), p.hover.Over(p.bg), p.text)
					b.Menu(func(m *ui.Menu) { a.batchAddMenu(m, sel) })
				}
				if a.route.page == pagePlaylist && a.editable(a.route.id) != nil && a.can("removeFromPlaylist") {
					if pillButton(c.Key("batch-remove"), iconTrash, T("Remove from Playlist"), p.hover.Over(p.bg), p.text).Clicked() {
						a.removeFromBatch(a.library, a.route.id, sel)
					}
				}
				if a.canDownloadAny(sel) {
					pillButton(c.Key("batch-download"), iconDownload, T("Download"), p.hover.Over(p.bg), p.text).
						Menu(func(m *ui.Menu) { a.downloadQualityMenu(m, sel) })
				}
				ui.Box(c).Width(0.5).Height(20).Background(p.hairline)
				if pillButton(c.Key("batch-all"), nil, T("Select All"), p.hover.Over(p.bg), p.text).Clicked() {
					for j := range a.pageTracks {
						a.trackSel.Add(j + 1)
					}
				}
				if pillButton(c.Key("batch-clear"), nil, T("Clear"), p.hover.Over(p.bg), p.text).Clicked() {
					a.trackSel.Clear()
				}
			})
	})
}

// batchAddMenu lists the listener's playlists to put all of sel in, but the
// one they are listed in.
func (a *app) batchAddMenu(m *ui.Menu, sel []music.Track) {
	lib := a.library
	pid := ""
	if a.route.page == pagePlaylist {
		pid = a.route.id
	}
	for i := range lib.mine {
		pl := &lib.mine[i]
		if pl.ID == pid || pl.Fixed {
			continue
		}
		if m.Item(pl.Name).Chosen() {
			a.addToBatch(lib, pl.ID, sel)
		}
	}
}

// addToBatch puts all of tracks in the listener's playlist pid; a toast says
// how it went.
func (a *app) addToBatch(lib *library, pid string, tracks []music.Track) {
	ids := make([]string, 0, len(tracks))
	for _, tr := range tracks {
		ids = append(ids, tr.ID)
	}
	n := len(ids)
	go func() {
		err := lib.src.AddToPlaylist(pid, ids)
		a.post(func() {
			if err != nil {
				a.actionFailed(lib, err, T("Couldn't add it to the playlist. Try again later."))
				return
			}
			a.toast = Tf("Added %d songs to the playlist", n)
			a.playlistChanged(lib, pid)
		})
	}()
}

// removeFromBatch takes all of tracks out of the listener's playlist pid, at
// once in the app and then with its source, which may refuse: they come back
// then.
func (a *app) removeFromBatch(lib *library, pid string, tracks []music.Track) {
	ids := make([]string, 0, len(tracks))
	gone := map[string]bool{}
	for _, tr := range tracks {
		ids = append(ids, tr.ID)
		gone[tr.ID] = true
	}
	n := len(ids)
	pl := lib.playlists[pid]
	var before []music.Track
	if pl != nil {
		before = pl.Tracks
		pl.Tracks = slices.DeleteFunc(slices.Clone(before), func(t music.Track) bool { return gone[t.ID] })
	}
	go func() {
		err := lib.src.RemoveFromPlaylist(pid, ids)
		a.post(func() {
			if err != nil {
				if pl != nil && lib.playlists[pid] == pl {
					pl.Tracks = before
				}
				a.actionFailed(lib, err, T("Couldn't remove it from the playlist. Try again later."))
				return
			}
			a.toast = Tf("Removed %d songs from the playlist", n)
			a.trackSel.Clear()
			a.loadMine(lib)
		})
	}()
}