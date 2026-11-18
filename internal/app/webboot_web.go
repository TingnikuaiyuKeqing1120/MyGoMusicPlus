//go:build js

package app

// webBoot connects the APK installer where the shell offers it and starts
// the daily look at the app's own releases.
func webBoot(a *app) {
	useAppInstall(a)
	a.keepCheckingAppUpdate()
}
