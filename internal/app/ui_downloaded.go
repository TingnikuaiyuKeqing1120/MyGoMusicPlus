package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// downloadedPage lists the songs saved to files of the listener's, newest
// first, played from disk. A click plays the list from there; Ctrl/Shift-click
// or the checkbox chooses several to delete at once, and each row can show its
// file in the file manager or delete it.
func (a *app) downloadedPage(c *ui.Context) {
	jobs := a.downloads.savedJobs()
	tracks := make([]music.Track, len(jobs))
	var total int64
	for i, j := range jobs {
		tracks[i] = downloadedTrack(j)
		total += j.size
	}
	a.pageTracks = tracks
	top, bottom := a.insets(c)
	a.underTop, a.underBottom = true, true

	header := func(c *ui.Context) {
		p := a.pal
		mediaHeader(c).Padding(0, 0, 26, 0).Children(func() {
			ui.Box(c).Size(mediaCoverSize(c, 212), mediaCoverSize(c, 212)).Radius(14).Center().Shadow(0, 18, 40, -10, p.cardShade).TextColor(white).
				LinearGradient(ui.LinearGradient{Angle: 135, Oklab: true, From: ui.Oklch(0.72, 0.15, 150), To: ui.Oklch(0.55, 0.18, 200)}).
				Children(func() { ui.Icon(c, iconDownload).Size(84, 84) })
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
				ui.Text(c, T("Library")).FontSize(12).FontWeight(600).TextColor(p.accent)
				ui.Text(c, T("Downloaded")).FontSize(30).FontWeight(800).LetterSpacing(-0.6)
				ui.Text(c, Tf("%d songs · %s", len(jobs), downloadSize(total))).FontSize(13).TextColor(p.muted)
				if len(jobs) > 0 {
					ui.Row(c).Wrap().Gap(10).Margin(10, 0, 0, 0).Children(func() {
						a.playButtons(c, tracks)
						if a.trackSel.Len() > 0 {
							if pillButton(c.Key("dl-delete"), iconTrash, T("Delete Selected"), p.hover.Over(p.bg), p.text).Clicked() {
								a.askDeleteDownloads(a.selectedTracks())
							}
							if pillButton(c.Key("dl-clear"), nil, T("Clear"), p.hover.Over(p.bg), p.text).Clicked() {
								a.trackSel.Clear()
							}
						}
					})
				}
			})
		})
	}

	var footer []func(c *ui.Context)
	if len(jobs) == 0 {
		footer = append(footer, func(c *ui.Context) { a.empty(c, T("Songs you download will show here")) })
	}
	rows := len(jobs) + 1 + len(footer)
	a.selList.Label = func(i int) string {
		if i >= 1 && i <= len(tracks) {
			return tracks[i-1].Name
		}
		return ""
	}
	// ListState.Header pins rows while scrolling. This page's artwork scrolls
	// away with its songs, so non-song selections are filtered after input.
	a.selList.Header = nil
	a.selList.Selection = &a.trackSel
	a.selList.MenuKeepsSelection = true
	a.selList.Reorder, a.selList.DragRows = nil, nil
	played := false
	theme := c.Theme()
	ui.Themed(c, trackListTheme(c, a.pal)).Grow(1).Children(func() {
		ui.List(c.Key(a.route), &a.selList, rows).ObserveInput(touchScrollInput).OnChange(func() {
			if played {
				a.trackSel.Clear()
			}
			a.trackSel.Remove(0)
			for i := len(jobs) + 1; i < rows; i++ {
				a.trackSel.Remove(i)
			}
		}).
			Grow(1).Padding(top+pageGap, pagePad-12, bottom+32, pagePad-12).ScrollbarInsets(top, 0, bottom, 0).
			Rows(func(r ui.ListRow) {
				ui.Themed(c, theme).Children(func() {
					i := r.Index
					if i == 0 {
						ui.Column(c).Children(func() {
							a.startMark(c)
							header(c)
						})
						return
					}
					if i == rows-1 {
						defer a.endMark(c)
					}
					if i > len(jobs) {
						footer[i-len(jobs)-1](c)
						return
					}
					played = a.downloadedRow(c, jobs, tracks, i-1, r) || played
				})
			})
	})
	a.selList.OnShortcut(c, 0, ui.KeyDelete, func() {
		a.askDeleteDownloads(a.selectedTracks())
	})
}

// downloadSize says how much a number of bytes takes, in GB or MB.
func downloadSize(n int64) string {
	if n >= 1<<30 {
		return Tf("%.1f GB", float64(n)/(1<<30))
	}
	return Tf("%.0f MB", float64(n)/(1<<20))
}

// downloadedRow is one saved song: its cover and credits, how much it takes,
// and its show-in-folder and delete, played from its file as it's clicked.
func (a *app) downloadedRow(c *ui.Context, jobs []*dlJob, tracks []music.Track, i int, r ui.ListRow) bool {
	p := a.pal
	tr := &tracks[i]
	j := jobs[i]
	playing := sameTrack(a.pl.Current(), tr)
	k := i + 1
	row := ui.Row(c).Height(54).PaddingX(12).Gap(14).Radius(10).AlignItems(ui.Center)
	compact := narrow(c)
	if compact {
		row.Height(64).PaddingX(6).Gap(8)
	}
	hovered := row.Hovered()
	bg := ui.Transparent
	switch {
	case playing:
		bg = p.accent.Alpha(0.09)
	case hovered:
		bg = p.hover
	}
	row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
	row.Children(func() {
		a.trackCheckbox(c, k, tr.Name)
		a.cover(c, tr.Album.ImageURL, 96, 38, 6).Shrink(0)
		ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
			name := p.text
			if playing {
				name = p.accent
			}
			ui.Text(c, tr.Name).FontSize(14).FontWeight(500).TextColor(name).SingleLine()
			ui.Text(c, tr.ArtistNames()).FontSize(12.5).TextColor(p.muted).SingleLine()
		})
		if !compact {
			ui.Textf(c, "%.1f MB", float64(j.size)/(1<<20)).FontSize(12.5).FontFeatures("tnum").TextColor(p.faint).Width(72).TextAlign(ui.End)
		}
		if !onWeb && !compact {
			ui.Box(c).Size(32, 32).Center().Children(func() {
				if iconButton(c.Key("reveal"), iconFolder, 15, p.muted, T("Show in Folder")).Clicked() {
					revealFile(j.path)
				}
			})
		}
		ui.Box(c).Size(32, 32).Center().Children(func() {
			if iconButton(c.Key("delete"), iconTrash, 15, p.muted, T("Delete")).Clicked() {
				a.askDeleteDownloads([]music.Track{*tr})
			}
		})
		if !compact {
			ui.Text(c, clock(tr.Length())).FontSize(12.5).FontFeatures("tnum").TextColor(p.faint).Width(44).TextAlign(ui.End)
		}
	})
	play := row.RowClicked()
	if play {
		a.trackSel.Clear()
		a.playTrack(tracks, i)
	}
	return play
}
