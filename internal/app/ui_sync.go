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
	s := &a.syncer
	if deviceStart != nil {
		a.deviceSignIn(c, row, p)
	}
	row(T("GitHub Token"), T("A personal access token with repo access, kept on this device alone"), func() {
		in := ui.TextInput(c.Key("sync-token"), &s.cfg.Token).Label(T("GitHub Token")).Placeholder("ghp_…").Password().Width(220)
		if in.Submitted() || !in.Focused() && s.cfg.Token != s.saved.Token {
			a.saveSyncConfig()
		}
	})
	row(T("Repository"), T("A private repository under your account, made if it isn't there"), func() {
		in := ui.TextInput(c.Key("sync-repo"), &s.cfg.Repo).Label(T("Repository")).Placeholder("mygo-music-sync").Width(220)
		if in.Submitted() || !in.Focused() && s.cfg.Repo != s.saved.Repo {
			a.saveSyncConfig()
		}
	})
	for _, u := range sync.Units {
		on := s.cfg.Units[u]
		label, desc := syncUnitText(u)
		row(label, desc, func() {
			if ui.Switch(c.Key("sync-"+string(u)), &on).Label(label).Changed() {
				s.cfg.Units[u] = on
				a.saveSyncConfig()
			}
		})
	}
	row(T("Sync Now"), syncStatusDesc(s), func() {
		if ui.Button(c.Key("sync-now"), T("Sync Now")).Disabled(s.busy || !s.cfg.Enabled()).Clicked() {
			a.syncNow()
		}
	})
	if s.err != "" {
		ui.Text(c, s.err).FontSize(12).TextColor(p.accent).LineHeight(1.4)
	}
}

// deviceSignIn is the GitHub sign-in of the device flow: a button to start
// it, then the code to enter at GitHub's device page while the app asks for
// the token, the listener never writing one by hand.
func (a *app) deviceSignIn(c *ui.Context, row settingsRow, p palette) {
	s := &a.syncer
	d := s.device
	if d == nil {
		row(T("Sign in with GitHub"), T("Authorize through GitHub's device page instead of writing a token"), func() {
			if ui.Button(c.Key("device-start"), T("Sign in with GitHub")).Clicked() {
				a.startDeviceFlow()
			}
		})
		return
	}
	row(T("GitHub Sign-in"), T("Enter the code at GitHub's device page; the token arrives here once you have"), func() {
		switch {
		case d.err != "":
			ui.Text(c, d.err).FontSize(12).TextColor(p.accent).LineHeight(1.4)
			if ui.Button(c.Key("device-retry"), T("Try Again")).Clicked() {
				a.startDeviceFlow()
			}
		case d.userCode == "":
			ui.Text(c, T("Asking GitHub…")).FontSize(13).TextColor(p.muted)
		default:
			ui.Text(c, d.userCode).FontSize(22).FontWeight(700)
			if ui.Button(c.Key("device-open"), T("Open GitHub's Device Page")).Clicked() {
				mygo.Shell.OpenExternal(d.uri)
			}
		}
		if ui.Button(c.Key("device-cancel"), T("Cancel")).Clicked() {
			a.cancelDeviceFlow()
		}
	})
}

// syncUnitText is a kind's name and what it covers.
func syncUnitText(u sync.Unit) (label, desc string) {
	switch u {
	case sync.UnitSettings:
		return T("Sync Settings"), T("Your preferences, like language, appearance and quality")
	case sync.UnitPlaylists:
		return T("Sync Playlists"), T("Your playlists and likes in the built-in source, merged across devices")
	default:
		return T("Sync Plugins"), T("The plugins you installed and their setup")
	}
}

// syncStatusDesc says when syncing last came through, or what it's doing.
func syncStatusDesc(s *syncState) string {
	if s.busy {
		return T("Syncing…")
	}
	if s.last.IsZero() {
		return T("Pulls what changed elsewhere, pushes what changed here")
	}
	return Tf("Last synced %s", i18n.Ago(s.last, time.Now()))
}