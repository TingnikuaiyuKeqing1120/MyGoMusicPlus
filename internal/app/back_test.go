package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The shell's back button closes what's on top first: the dialogs, the
// drawer, the settings one page at a time, Now Playing, then the page
// before; at the root it leaves the button to the system, which exits.
func TestShellBack(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	a.installer.open = true
	a.naming.open = true
	a.deleting.open = true
	a.signIn.open = true
	a.navOpen = true
	a.settingsWeb, a.settingsPage, a.settingsQuery = true, "plugins", "gd"
	a.nowPlaying = true
	a.history = []route{{page: pageHome, src: "fake"}}
	a.route = route{page: pageLiked, src: "fake"}

	step := func() {
		t.Helper()
		if !a.shellBack() {
			t.Fatal("a step was left to take, but the button went to the system")
		}
		runPost(t, ch)
	}
	step()
	if a.installer.open {
		t.Error("the install dialog stayed open over everything")
	}
	step()
	if a.naming.open {
		t.Error("the playlist-name dialog stayed open")
	}
	step()
	if a.deleting.open {
		t.Error("the delete dialog stayed open")
	}
	step()
	if a.signIn.open {
		t.Error("the sign-in dialog stayed open")
	}
	step()
	if a.navOpen {
		t.Error("the drawer stayed open over the settings")
	}
	step()
	if a.settingsPage != "" || a.settingsQuery != "" || !a.settingsWeb {
		t.Errorf("settings page %q query %q web %v, not back at the list", a.settingsPage, a.settingsQuery, a.settingsWeb)
	}
	step()
	if a.settingsWeb {
		t.Error("the settings stayed open at their list")
	}
	step()
	if a.nowPlaying {
		t.Error("Now Playing stayed covering the page")
	}
	step()
	if a.route.page != pageHome || len(a.history) != 0 {
		t.Errorf("went to %+v with %d behind, not the page before", a.route, len(a.history))
	}
	if a.shellBack() {
		t.Error("at the root the app kept the button instead of leaving the app")
	}
}

// Back in the phone settings goes to the section list, then to the player.
func TestNarrowSettingsBack(t *testing.T) {
	a := newTestApp()
	a.settingsWeb, a.settingsPage = true, "general"
	was := onWeb
	onWeb = true
	defer func() { onWeb = was }()
	tt := ui.NewTester(a.view, 390, 780)
	tt.Frame()
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.settingsWeb || a.settingsPage != "" {
		t.Fatal("Back didn't return from General to the settings list")
	}
	if err := tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	if a.settingsWeb {
		t.Error("Back from the settings list left the settings open")
	}
}
