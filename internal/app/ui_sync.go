package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-music/internal/i18n"
	"mygo-music/internal/sync"
)

// syncSettings is the Sync page: the token and repository, which kinds sync,
// and a button to sync now. The token stays on this device, never synced.
func (a *app) syncSettings(c *ui.Context, row settingsRow, p palette) {
	a.importSettings(c, row)
	s := &a.syncer
	if deviceStart != nil {
		a.deviceSignIn(c, row, p)
	}
	row(T("GitHub Token"), T("A personal access token with repo access, kept on this device alone"), func() {
		in := ui.TextInput(c.Key("sync-token"), &s.cfg.Token).Label(T("GitHub Token")).Placeholder("ghp_…").Password().Disabled(s.busy).Width(220).MaxWidthPercent(100)
		if in.Submitted() || !in.Focused() && s.cfg.Token != s.saved.Token {
			a.saveSyncConfig()
		}
	})
	row(T("Repository"), T("A private repository under your account, made if it isn't there"), func() {
		in := ui.TextInput(c.Key("sync-repo"), &s.cfg.Repo).Label(T("Repository")).Placeholder("mygo-music-sync").Disabled(s.busy).Width(220).MaxWidthPercent(100)
		if in.Submitted() || !in.Focused() && s.cfg.Repo != s.saved.Repo {
			a.saveSyncConfig()
		}
	})
	for _, u := range sync.Units {
		on := s.cfg.Units[u]
		label, desc := syncUnitText(u)
		row(label, desc, func() {
			ui.Switch(c.Key("sync-"+string(u)), &on).Label(label).Disabled(s.busy).OnClick(func() {
				if s.cfg.Units == nil {
					s.cfg.Units = map[sync.Unit]bool{}
				}
				s.cfg.Units[u] = on
				a.saveSyncConfig()
			})
		})
	}
	row(T("Sync Now"), syncStatusDesc(s), func() {
		ui.Column(c).MaxWidthPercent(100).Gap(5).Children(func() {
			if ui.Button(c.Key("sync-now"), T("Sync Now")).Disabled(s.busy || !s.cfg.Enabled()).Clicked() {
				a.syncNow()
			}
			if s.busy && s.total > 0 {
				ui.Progress(c.Key("sync-progress"), float64(s.done)/float64(s.total)).Width(150).MaxWidthPercent(100).Label(syncStatusDesc(s))
			}
			if s.err != "" {
				ui.Text(c, s.err).FontSize(12).TextColor(p.accent).LineHeight(1.4)
			}
		})
	})
}

// deviceSignIn is the GitHub sign-in of the device flow: a button to start
// it, then the code to enter at GitHub's device page while the app asks for
// the token, the listener never writing one by hand.
func (a *app) deviceSignIn(c *ui.Context, row settingsRow, p palette) {
	s := &a.syncer
	d := s.device
	if d == nil {
		desc := T("Authorize through GitHub's device page instead of writing a token")
		if s.cfg.Token != "" {
			desc = T("A GitHub token is set for sync on this device.")
		}
		row(T("GitHub Sign-in"), desc, func() {
			if ui.Button(c.Key("device-start"), T("Sign in with GitHub")).Disabled(s.busy).Clicked() {
				a.startDeviceFlow()
			}
		})
		return
	}
	desc := ""
	if d.err == "" && d.userCode != "" {
		desc = T("Enter the code at GitHub, then return here to finish signing in.")
	}
	row(T("GitHub Sign-in"), desc, func() {
		ui.Column(c).Gap(8).AlignItems(ui.Start).MaxWidthPercent(100).Children(func() {
			switch {
			case d.err != "":
				ui.Text(c, d.err).FontSize(12).TextColor(p.accent).LineHeight(1.4)
				ui.Button(c.Key("device-retry"), T("Try Again")).OnClick(a.startDeviceFlow)
			case d.userCode == "":
				ui.Text(c, T("Asking GitHub…")).FontSize(13).TextColor(p.muted)
			default:
				ui.Text(c, d.userCode).FontSize(22).FontWeight(700)
				ui.Button(c.Key("device-copy"), T("Copy code")).OnClick(func() {
					if err := copyDeviceCode(d.userCode); err != nil {
						d.copyStatus = T("Couldn't copy the code.")
					} else {
						d.copyStatus = T("Code copied")
					}
				})
				if d.copyStatus != "" {
					ui.Text(c, d.copyStatus).FontSize(12).TextColor(p.muted)
				}
				ui.Text(c, T("Waiting for GitHub to confirm authorization…")).FontSize(12).TextColor(p.muted)
				ui.Button(c.Key("device-open"), T("Open GitHub's Device Page")).OnClick(func() { a.openDevicePage(d.uri, true) })
				if openGitHubDevicePage != nil {
					ui.Button(c.Key("device-browser"), T("Open in Browser")).OnClick(func() { a.openDevicePage(d.uri, false) })
				}
			}
			ui.Button(c.Key("device-cancel"), T("Cancel")).OnClick(a.cancelDeviceFlow)
		})
	})
}

func (a *app) openDevicePage(uri string, preferApp bool) {
	if openGitHubDevicePage != nil {
		if openGitHubDevicePage(preferApp) {
			return
		}
	} else if mygo.Shell.OpenExternal(uri) == nil {
		return
	}
	a.toast = T("Couldn't open GitHub's device page.")
}

// syncUnitText is a kind's name and what it covers.
func syncUnitText(u sync.Unit) (label, desc string) {
	switch u {
	case sync.UnitSettings:
		return T("Sync Settings"), T("Preferences, search history and playback queue")
	case sync.UnitPlaylists:
		return T("Sync Playlists"), T("Local playlists, likes and listening history")
	default:
		return T("Sync Plugins and Data"), T("All plugins, their settings and saved data, including sign-ins")
	}
}

// syncStatusDesc says when syncing last came through, or what it's doing.
func syncStatusDesc(s *syncState) string {
	if s.busy {
		if s.total > 0 {
			return Tf("%s (%d/%d)", T(s.phase), s.done, s.total)
		}
		return T(s.phase)
	}
	if s.last.IsZero() {
		return T("Pulls what changed elsewhere, pushes what changed here")
	}
	last := Tf("Last synced %s", i18n.Ago(s.last, time.Now()))
	if s.summary != "" {
		return last + " · " + s.summary
	}
	return last
}
