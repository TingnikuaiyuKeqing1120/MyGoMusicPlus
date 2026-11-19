package app

import (
	"github.com/egoist/mygo/ui"
	"mygo-music/internal/music"
)

// mediaHeader stacks artwork and details where a desktop header would squeeze
// the title and its actions beyond the phone's viewport.
func mediaHeader(c *ui.Context) ui.Element {
	if narrow(c) {
		return ui.Column(c).Gap(16).AlignItems(ui.Stretch)
	}
	return ui.Row(c).Gap(28).AlignItems(ui.End)
}

func mediaCoverSize(c *ui.Context, desktop float32) float32 {
	if narrow(c) {
		return 112
	}
	return desktop
}

// mobilePlayerBar keeps transport and the current song inside the phone width.
func (a *app) mobilePlayerBar(c *ui.Context) {
	p, pl := a.pal, a.pl
	tr := pl.Current()
	ui.Box(c.Key("mobile-player")).Absolute().Left(0).Right(0).Bottom(0).Height(playerBarHeight).
		Background(p.bg).Children(func() {
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(1).Background(p.hairline)
		a.scrubber(c.Key("bar-scrub"), &a.barScrub, p.press, p.accent, 12, ui.Transparent).
			Absolute().Left(12).Right(12).Top(0)
		ui.Row(c).Fill().Padding(12, 12, 4, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			info := ui.ButtonBase(c.Key("info")).Grow(1).MinWidth(0).Gap(10).AlignItems(ui.Center).
				Label(T("Open Now Playing")).Disabled(tr == nil)
			info.Children(func() {
				if tr == nil {
					ui.Text(c, T("Not Playing")).FontSize(13).TextColor(p.muted)
					return
				}
				cover := a.cover(c, tr.Album.ImageURL, 96, 42, 7).Shrink(0)
				if b := cover.LayoutBounds(); b.W > 0 {
					a.npFrom = b
				}
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, tr.Name).FontSize(13.5).FontWeight(600).SingleLine()
					sub := tr.ArtistNames()
					if pl.Loading() {
						sub = T("Buffering…")
					} else if pl.Err() != "" {
						sub = pl.Err()
					}
					ui.Text(c, sub).FontSize(11.5).TextColor(p.muted).SingleLine()
				})
			})
			info.OnClick(func() { a.nowPlaying = true })
			playButton(c.Key("play"), pl.Playing(), 28, p.text).OnClick(pl.TogglePlay)
			iconButton(c.Key("next"), iconNext, 20, p.text, T("Next")).OnClick(pl.Next)
			iconButton(c.Key("queue"), iconQueue, 18, p.muted, T("Queue")).Disabled(tr == nil).
				OnClick(func() { a.nowPlaying, a.npPane = true, paneQueue })
		})
	})
}

// mobileNowPlaying keeps the song controls above a full-width lyrics or queue
// pane, with tabs and the close button available to touch.
func (a *app) mobileNowPlaying(c *ui.Context, tr *music.Track, art *artwork, accent ui.Color, w, h float32) {
	pl := a.pl
	ui.Row(c).Height(40).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		iconButton(c.Key("close"), iconDown, 22, white.Alpha(0.8), T("Close")).OnClick(func() { a.nowPlaying = false })
	})
	ui.Row(c).Padding(4, 16).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(72, 72).Shrink(0).Children(func() { a.npCover(c, tr, art, 72) })
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			if radio := pl.Radio(); radio != "" {
				a.radioChip(c, radio)
			}
			ui.Text(c, tr.Name).FontSize(18).FontWeight(700).MaxLines(2).LineHeight(1.2)
			a.artistLinks(c, tr, white.Alpha(0.6), white, true).FontSize(12).SingleLine()
		})
		a.likeButton(c.Key("like"), tr, 18, white.Alpha(0.7))
		a.songMenu(c.Key("more"), tr, 18, white.Alpha(0.7))
	})
	ui.Column(c).Padding(8, 16, 0, 16).Children(func() {
		a.scrubber(c.Key("np-scrub"), &a.npScrub, white.Alpha(0.18), white, 16, ui.Transparent)
		ui.Row(c).Children(func() {
			ui.Text(c, clock(pl.Position())).FontSize(11).TextColor(white.Alpha(0.6))
			ui.Spacer(c)
			status := clock(pl.Duration())
			if pl.Loading() {
				status = T("Buffering…")
			}
			ui.Text(c, status).FontSize(11).TextColor(white.Alpha(0.6))
		})
		a.transport(c, white, white.Alpha(0.5), accent, 24)
	})
	ui.Row(c.Key("tabs")).Wrap().Gap(2).Padding(6, 8).Children(func() {
		a.paneTabs(c)
		if a.npPane == paneStage {
			a.stageStyleButton(c)
		}
	})
	pane := ui.Column(c.Key("pane")).Grow(1).MinHeight(0).Padding(0, 12, 12, 12).Clip()
	bounds := pane.Bounds()
	paneW, paneH := w-24, max(80, h-280)
	if bounds.W > 0 && bounds.H > 0 {
		paneW, paneH = bounds.W, bounds.H
	}
	pane.Children(func() {
		switch a.npPane {
		case paneLyrics:
			a.lyricsPane(c, tr, paneH)
		case paneStage:
			a.stage(c, tr, accent)
		case paneComments:
			a.commentsPane(c, tr)
		case paneShow:
			a.showPane(c, tr)
		case paneCaptions:
			a.captionsPane(c, tr, paneH)
		case paneWall:
			a.wall(c, 0, paneW, paneH, 0, 0, 0)
		default:
			a.queuePane(c)
		}
	})
}
