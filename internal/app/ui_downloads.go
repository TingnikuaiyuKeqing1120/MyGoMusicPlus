package app

import (
	"github.com/egoist/mygo/ui"
)

// downloadButton sits in the player bar: the download manager's panel, with a
// mark of how many songs are on their way.
func (a *app) downloadButton(c *ui.Context, color ui.Color) {
	p := a.pal
	ui.Box(c).Size(32, 32).Children(func() {
		b := iconButton(c.Key("downloads-button"), iconDownload, 16, color, T("Downloads"))
		if b.Clicked() {
			a.downloads.open = !a.downloads.open
		}
		if n := a.downloads.working(); n > 0 {
			ui.Box(c).Absolute().Top(0).Right(0).Size(15, 15).Radius(8).Background(p.accent).Center().PassThrough().Children(func() {
				ui.Textf(c, "%d", n).FontSize(9).FontWeight(700).TextColor(p.onAccent)
			})
		}
		pop := popoverColors(p.dark)
		ui.PopoverBase(c.Key("downloads-popover"), b, &a.downloads.open, func(panel ui.Element) {
			panel.AttachTo(b, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 10, 0).Width(360).Padding(10).Gap(8).
				Radius(12).Background(pop.fill).Border(0.5, pop.edge).Shadow(0, 8, 24, -4, ui.RGBA(0, 0, 0, 0.2)).Label(T("Downloads"))
			a.downloadsPanel(c, pop)
		})
	})
}

// downloadsPanel lists the songs on their way to files, each with its progress
// and what can be done with it.
func (a *app) downloadsPanel(c *ui.Context, pop popover) {
	p := a.pal
	m := &a.downloads
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, T("Downloads")).FontSize(13).FontWeight(700).Grow(1)
		if m.anyFinished() {
			if pillButton(c.Key("dl-clear"), nil, T("Clear"), pop.track, p.text).Height(26).PaddingX(10).Clicked() {
				m.clearFinished()
			}
		}
	})
	if len(m.jobs) == 0 {
		ui.Text(c, T("No downloads yet")).FontSize(12.5).TextColor(p.muted).Padding(8, 0)
		return
	}
	ui.Scroll(c).MaxHeight(360).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			for _, j := range m.jobs {
				a.downloadRow(c, j, pop)
			}
		})
	})
}

// downloadRow is one song in the panel: its name, how far it has come or what
// became of it, and its cancel, retry or remove.
func (a *app) downloadRow(c *ui.Context, j *dlJob, pop popover) {
	p := a.pal
	m := &a.downloads
	ui.Row(c.Key(j.id)).Gap(8).AlignItems(ui.Center).Padding(5, 6).Radius(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, j.track.Name).FontSize(13).FontWeight(500).SingleLine()
			switch j.status {
			case dlDownloading:
				frac := float32(0)
				if j.size > 0 {
					frac = float32(float64(j.done) / float64(j.size))
				}
				ui.Box(c).Height(4).Radius(2).Grow(1).Draw(func(pt *ui.Painter, r ui.Rect) {
					pt.Fill(r, pop.track, 2)
					if frac > 0 {
						pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * min(frac, 1), H: r.H}, p.accent, 2)
					}
				})
			case dlQueued:
				ui.Text(c, T("Queued")).FontSize(11.5).TextColor(p.muted)
			case dlPaused:
				ui.Text(c, T("Paused")).FontSize(11.5).TextColor(p.muted)
			case dlDone:
				ui.Textf(c, "%.1f MB", float64(j.size)/(1<<20)).FontSize(11.5).TextColor(p.muted)
			case dlFailed:
				ui.Text(c, j.err).FontSize(11.5).TextColor(ui.Hex("#e5484d")).SingleLine()
			case dlCanceled:
				ui.Text(c, T("Canceled")).FontSize(11.5).TextColor(p.muted)
			}
		})
		switch j.status {
		case dlQueued:
			if iconButton(c.Key("cancel"), iconX, 13, p.muted, T("Cancel")).Clicked() {
				m.cancel(j.id)
			}
		case dlDownloading:
			if iconButton(c.Key("pause"), iconPause, 13, p.muted, T("Pause download")).Clicked() {
				m.pause(j.id)
			}
			if iconButton(c.Key("cancel"), iconX, 13, p.muted, T("Cancel")).Clicked() {
				m.cancel(j.id)
			}
		case dlPaused:
			if iconButton(c.Key("resume"), iconPlay, 13, p.muted, T("Resume download")).Clicked() {
				m.resume(a, j.id)
			}
			if iconButton(c.Key("cancel"), iconX, 13, p.muted, T("Cancel")).Clicked() {
				m.cancel(j.id)
			}
		case dlFailed, dlCanceled:
			if iconButton(c.Key("retry"), iconRefresh, 13, p.muted, T("Retry")).Clicked() {
				m.retry(a, j.id)
			}
		case dlDone:
			if iconButton(c.Key("remove"), iconX, 13, p.muted, T("Remove")).Clicked() {
				m.remove(j.id)
			}
		}
	})
}