package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo"
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
	if u := &a.appUpd; u.version != "v0.3.0" || u.apk != "https://example.com/app.apk" {
		t.Fatalf("the newer release shows %+v", u)
	}
	fetchAppRelease = func() (*appRelease, error) {
		return &appRelease{Tag: "v0.2.0"}, nil
	}
	a.checkAppUpdate()
	runPost(t, ch)
	if u := &a.appUpd; u.version != "" || u.apk != "" {
		t.Fatalf("the current version offers %+v", u)
	}
}

// The update goes to the shell's installer, the download showing while it's
// on its way.
func TestAppUpdateInstall(t *testing.T) {
	a := newTestApp()
	was := installApk
	defer func() { installApk = was }()
	var got string
	installApk = func(url string) { got = url }
	a.appUpd.apk = "https://example.com/app.apk"
	a.installAppUpdate()
	if got != "https://example.com/app.apk" || a.appUpd.install != "downloading" {
		t.Fatalf("handed %q, state %q", got, a.appUpd.install)
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