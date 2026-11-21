//go:build js

package app

import "github.com/egoist/mygo"

func init() {
	// WASM has no OS user config directory. Reuse the persistent filesystem's
	// existing profile so downloads and playback sessions can use App.Path too.
	mygo.App.SetPath(mygo.PathUserData, userDataDir(""))
}

// webBoot connects the APK installer where the shell offers it and starts
// the daily look at the app's own releases.
func webBoot(a *app) {
	a.applyCacheLimit()
	useAppInstall(a)
	a.keepCheckingAppUpdate()
}
