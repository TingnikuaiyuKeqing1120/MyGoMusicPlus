package app

// shellBack takes one step back for the shell's back button, as Android's:
// the dialog on top closes first, then the drawer, the settings' page, Now
// Playing, the page before. It answers whether there was a step to take;
// without one the shell lets the system have the button, which leaves the app.
func (a *app) shellBack() bool {
	var step func()
	switch {
	case a.installer.open:
		step = func() {
			a.installer.open = false
			a.cancelInstall()
		}
	case a.naming.open:
		step = func() { a.naming.open = false }
	case a.deleting.open:
		step = func() { a.deleting.open = false }
	case a.signIn.open:
		step = func() {
			a.signIn.open = false
			if a.signIn.lib != nil {
				a.closeSignInPage(a.signIn.lib.id)
			}
		}
	case a.navOpen:
		step = func() { a.navOpen = false }
	case a.settingsWeb && a.settingsPage != "":
		step = func() { a.settingsPage, a.settingsQuery = "", "" }
	case a.settingsWeb:
		step = func() { a.settingsWeb = false }
	case a.nowPlaying || len(a.history) > 0:
		step = a.back
	default:
		return false
	}
	a.post(step)
	return true
}