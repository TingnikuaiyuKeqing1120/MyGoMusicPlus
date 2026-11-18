package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Checking the app's releases keeps a newer one to offer, and nothing when
// what's running is current.
func TestAppUpdateCheck(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	was := fetchAppRelease
	defer func() { fetchAppRelease = was }()
	wasV := mygo.App.Version()
	defer mygo.App.SetVersion(wasV)
	mygo.App.SetVersion("0.2.0")
	fetchAppRelease = func() (*appRelease, error) {
		return &appRelease{Tag: "v0.3.0", APK: "https://example.com/app.apk", Page: "https://example.com/release"}, nil
	}
	a.checkAppUpdate()
	runPost(t, ch)
	if u := a.appUpd.release; u == nil || u.Tag != "v0.3.0" || u.APK != "https://example.com/app.apk" {
		t.Fatalf("the newer release shows %+v", u)
	}
	fetchAppRelease = func() (*appRelease, error) {
		return &appRelease{Tag: "v0.2.0"}, nil
	}
	a.checkAppUpdate()
	runPost(t, ch)
	if u := a.appUpd.release; u != nil {
		t.Fatalf("the current version offers %+v", u)
	}
}

// The shell gets the chosen route and the release's APK metadata.
func TestAppUpdateInstall(t *testing.T) {
	was := downloadApk
	defer func() { downloadApk = was }()
	for _, mirror := range []bool{false, true} {
		a := newTestApp()
		a.settings.UpdateMirror = mirror
		rel := appRelease{Tag: "v0.3.0", APK: "https://example.com/app.apk", Digest: "sha256:abc", Size: 120}
		var got appRelease
		var usingMirror bool
		downloadApk = func(r appRelease, m bool) { got, usingMirror = r, m }
		a.appUpd.release = &rel
		a.installAppUpdate()
		if got != rel || usingMirror != mirror || !a.appUpd.downloading() {
			t.Fatalf("handed %+v, mirror %v, state %q", got, usingMirror, a.appUpd.install)
		}
	}
}

// A cold start used to lose the install button until it checked and downloaded again.
func TestInstallDownloadedUpdateAfterRestart(t *testing.T) {
	a := newTestApp()
	a.settingsWeb, a.settingsPage = true, "updates"
	wasWeb, wasDownload, wasReopen := onWeb, downloadApk, reopenApkInstaller
	onWeb = true
	defer func() { onWeb, downloadApk, reopenApkInstaller = wasWeb, wasDownload, wasReopen }()
	downloadApk = func(appRelease, bool) { t.Error("installing a saved APK started a download") }
	opened := 0
	reopenApkInstaller = func() { opened++ }
	a.appUpd.restoreInstallState(`{"state":"","ready":"v0.3.0"}`)
	tt := ui.NewTester(a.view, 390, 780)
	tt.Frame()
	for range 2 { // The installer can be cancelled and reopened again.
		if err := tt.Click("Install Downloaded Update"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if opened != 2 || a.appUpd.release != nil {
		t.Fatalf("reopened %d times, release check produced %v", opened, a.appUpd.release)
	}
}

// The download's percent comes out of the shell's state, and a download of
// unknown size shows no fraction.
func TestAppUpdateDownloadProgress(t *testing.T) {
	u := &appUpdate{install: "downloading:45"}
	if frac, known := u.downloadProgress(); !known || frac != 0.45 {
		t.Fatalf("45%% came out %v %v", frac, known)
	}
	u.install = "downloading"
	if _, known := u.downloadProgress(); known {
		t.Error("a download of unknown size claimed a fraction")
	}
	u.install = "downloading:120"
	if frac, _ := u.downloadProgress(); frac != 1 {
		t.Errorf("over 100%% came out %v", frac)
	}
}

// The daily check asks once a day, not again within it.
func TestAppUpdateDaily(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	was := fetchAppRelease
	defer func() { fetchAppRelease = was }()
	calls := 0
	fetchAppRelease = func() (*appRelease, error) {
		calls++
		return &appRelease{Tag: "v0.0.1"}, nil
	}
	a.settings.AppChecked = time.Now()
	a.checkAppUpdateDaily()
	if calls != 0 {
		t.Fatal("checked again within the day")
	}
	a.settings.AppChecked = time.Time{}
	a.checkAppUpdateDaily()
	runPost(t, ch)
	if calls != 1 || a.settings.AppChecked.IsZero() {
		t.Fatalf("asked %d times, checked at %v", calls, a.settings.AppChecked)
	}
}
