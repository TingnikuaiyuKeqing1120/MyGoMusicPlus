//go:build js

package app

// webBoot takes what the web build has of the shell: the GitHub sign-in and
// the APK installer where the Android shell offers them, and the daily look
// at the app's own releases.
func webBoot(a *app) {
	useDeviceFlow()
	useAppInstall(a)
	a.keepCheckingAppUpdate()
}