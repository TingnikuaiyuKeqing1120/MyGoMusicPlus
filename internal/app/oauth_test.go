package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-music/internal/i18n"
	cloud "mygo-music/internal/sync"
)

// The phone's sync page keeps both authorization routes and cancellation reachable.
func TestDeviceFlowSyncPage(t *testing.T) {
	wasWeb, wasStart, wasOpen := onWeb, deviceStart, openGitHubDevicePage
	lang := i18n.Language()
	t.Cleanup(func() {
		onWeb, deviceStart, openGitHubDevicePage = wasWeb, wasStart, wasOpen
		i18n.SetLanguage(lang)
	})
	onWeb = true
	i18n.SetLanguage("zh-CN")
	deviceStart = func(string, string) (string, error) { return "", errors.New("unexpected start") }
	var routes []bool
	openGitHubDevicePage = func(preferApp bool) bool { routes = append(routes, preferApp); return true }

	a := newTestApp()
	a.settingsWeb, a.settingsPage = true, "sync"
	a.syncer.device = &deviceFlow{userCode: "TEST-CODE", uri: "https://github.com/login/device"}
	var copied string
	previous := copyDeviceCode
	t.Cleanup(func() { copyDeviceCode = previous })
	copyDeviceCode = func(code string) error { copied = code; return nil }
	tt := ui.NewTester(a.view, 320, 720)
	tt.SetDark(true)
	settled(tt)
	if dir := os.Getenv("MM_FRAMES"); dir != "" {
		saveFrame(t, dir, "github-device-sync-narrow.png", tt.Image())
	}
	for _, name := range []string{"Copy code", "Open GitHub's Device Page", "Open in Browser", "Cancel"} {
		if err := tt.Click(T(name)); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	if copied != "TEST-CODE" {
		t.Fatalf("copied %q", copied)
	}
	if !slices.Equal(routes, []bool{true, false}) || a.syncer.device != nil {
		t.Fatalf("opened routes %v, device flow still active: %t", routes, a.syncer.device != nil)
	}
}

// The device flow signs in to GitHub without a token written by hand: the
// code shows, the asking waits while GitHub says pending, and the token
// lands in the sync config once the listener has authorized.
func TestDeviceFlowSignIn(t *testing.T) {
	a := newTestApp()
	a.syncer.cfg.Token = "old-token"
	a.syncer.saved = a.syncer.cfg
	a.syncer.authFailed = true
	a.syncer.err = "previous sign-in rejected"
	ch := posts(a)
	wasStart, wasPoll := deviceStart, devicePoll
	defer func() { deviceStart, devicePoll = wasStart, wasPoll }()
	deviceStart = func(clientID, scope string) (string, error) {
		if clientID != githubClientID || scope != "repo" {
			t.Errorf("asked to start with %q %q", clientID, scope)
		}
		return `{"device_code":"dc","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":3600}`, nil
	}
	devicePoll = func(clientID, code string) (string, error) {
		if code != "dc" {
			t.Errorf("asked to poll with %q", code)
		}
		return `{"error":"authorization_pending"}`, nil
	}
	a.startDeviceFlow()
	runPost(t, ch)
	d := a.syncer.device
	if d == nil || d.userCode != "ABCD-1234" || d.uri != "https://github.com/login/device" {
		t.Fatalf("the sign-in shows %+v", d)
	}
	gen := a.syncer.deviceGen
	a.pollDeviceFlow(gen)
	runPost(t, ch)
	if a.syncer.device == nil {
		t.Fatal("the flow ended while GitHub said pending")
	}
	devicePoll = func(string, string) (string, error) {
		return `{"access_token":"gho_test"}`, nil
	}
	a.pollDeviceFlow(gen)
	runPost(t, ch)
	if a.syncer.device != nil {
		t.Error("the flow stayed open after the token came")
	}
	if a.syncer.cfg.Token != "gho_test" || a.syncer.saved.Token != "gho_test" {
		t.Errorf("the token is %q, kept %q", a.syncer.cfg.Token, a.syncer.saved.Token)
	}
	if a.syncer.authFailed || a.syncer.err != "" || loadSyncConfig().Token != "gho_test" {
		t.Fatal("reauthorization did not persist or left the previous error")
	}
}

// A failed token write keeps the prior credentials and reports the failure
// instead of claiming a sign-in that would disappear after restarting.
func TestDeviceFlowTokenSaveFails(t *testing.T) {
	root := userDataDir("")
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	t.Cleanup(func() { mygo.App.SetPath(mygo.PathUserData, root) })
	if err := os.Mkdir(syncConfigPath(), 0700); err != nil {
		t.Fatal(err)
	}
	previous := devicePoll
	t.Cleanup(func() { devicePoll = previous })
	devicePoll = func(string, string) (string, error) {
		return `{"access_token":"new-token"}`, nil
	}
	a := newTestApp()
	ch := posts(a)
	a.syncer.cfg.Token = "old-token"
	a.syncer.saved = a.syncer.cfg
	a.syncer.device = &deviceFlow{code: "fixture", expires: time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC)}
	a.pollDeviceFlow(a.syncer.deviceGen)
	runPost(t, ch)
	if a.syncer.device == nil || a.syncer.device.err == "" || a.toast != "" {
		t.Fatal("failed token write was reported as a successful sign-in")
	}
	if a.syncer.cfg.Token != "old-token" || a.syncer.saved.Token != "old-token" {
		t.Fatal("failed sign-in replaced the prior credentials")
	}
}

// A phone reports rejected credentials, keeps reauthorization reachable and
// resumes sync with the token obtained through that sign-in button.
func TestPhoneSyncReauthorization(t *testing.T) {
	root, language := userDataDir(""), i18n.Language()
	wasWeb, wasStart, wasPoll, wasRepository := onWeb, deviceStart, devicePoll, newSyncRepository
	t.Cleanup(func() {
		mygo.App.SetPath(mygo.PathUserData, root)
		i18n.SetLanguage(language)
		onWeb, deviceStart, devicePoll, newSyncRepository = wasWeb, wasStart, wasPoll, wasRepository
	})
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	i18n.SetLanguage("zh-CN")
	onWeb = true
	deviceStart = func(string, string) (string, error) {
		return `{"device_code":"fixture","user_code":"TEST-CODE","verification_uri":"https://github.com/login/device","expires_in":900,"interval":3600}`, nil
	}
	devicePoll = func(string, string) (string, error) {
		return `{"access_token":"renewed"}`, nil
	}
	newSyncRepository = func(token, repo string) *cloud.GitHub {
		g := cloud.NewGitHub(token, repo)
		g.Client.Transport = syncTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer renewed" {
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"message":"Bad credentials"}`)
				return
			}
			switch r.URL.Path {
			case "/user":
				fmt.Fprint(w, `{"login":"fixture"}`)
			case "/repos/fixture/sync":
				fmt.Fprint(w, `{}`)
			default:
				t.Errorf("unexpected request: %s", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		})}
		return g
	}
	a := newTestApp()
	a.settingsWeb, a.settingsPage = true, "sync"
	a.syncer.cfg = cloud.Config{Token: "rejected", Repo: "sync", Units: map[cloud.Unit]bool{cloud.UnitPlaylists: true}}
	a.syncer.saved = a.syncer.cfg
	ch := posts(a)
	a.syncNow()
	runPost(t, ch)
	if a.toast == "" || !a.syncer.authFailed {
		t.Fatal("phone authentication failure stayed silent")
	}
	tt := ui.NewTester(a.view, 320, 720)
	tt.SetDark(true)
	settled(tt)
	if dir := os.Getenv("MM_FRAMES"); dir != "" {
		saveFrame(t, dir, "github-reauthorize-phone.png", tt.Image())
	}
	if err := tt.Click(T("Sign in with GitHub")); err != nil {
		t.Fatal(err)
	}
	runPost(t, ch)
	if a.syncer.device == nil || a.syncer.device.code != "fixture" {
		t.Fatal("phone sign-in button did not start authorization")
	}
	a.syncNow()
	if a.syncer.busy {
		t.Fatal("sync used the old credentials while reauthorization was pending")
	}
	a.pollDeviceFlow(a.syncer.deviceGen)
	runPost(t, ch)
	tt.Frame()
	if a.syncer.authFailed || a.syncer.err != "" || loadSyncConfig().Token != "renewed" {
		t.Fatal("phone reauthorization did not restore sync credentials")
	}
	a.syncNow()
	for a.syncer.busy {
		runPost(t, ch)
	}
	if a.syncer.err != "" || a.syncer.last.IsZero() {
		t.Fatalf("phone sync after reauthorization: %s", a.syncer.err)
	}
}

// A sign-in GitHub refuses or lets expire says so and stops asking; one put
// away ignores the answer that comes back late.
func TestDeviceFlowRefusedAndCancelled(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	wasStart, wasPoll := deviceStart, devicePoll
	defer func() { deviceStart, devicePoll = wasStart, wasPoll }()
	deviceStart = func(string, string) (string, error) {
		return `{"device_code":"dc","user_code":"ABCD-1234","verification_uri":"u","expires_in":900,"interval":3600}`, nil
	}
	devicePoll = func(string, string) (string, error) {
		return `{"error":"access_denied"}`, nil
	}
	a.startDeviceFlow()
	runPost(t, ch)
	a.pollDeviceFlow(a.syncer.deviceGen)
	runPost(t, ch)
	if d := a.syncer.device; d == nil || d.err == "" {
		t.Fatalf("the refusal shows %+v", d)
	}

	devicePoll = func(string, string) (string, error) {
		return `{"error":"expired_token"}`, nil
	}
	a.startDeviceFlow()
	runPost(t, ch)
	a.pollDeviceFlow(a.syncer.deviceGen)
	runPost(t, ch)
	if d := a.syncer.device; d == nil || d.err == "" {
		t.Fatalf("the expiry shows %+v", d)
	}

	a.startDeviceFlow()
	runPost(t, ch)
	gen := a.syncer.deviceGen
	a.cancelDeviceFlow()
	devicePoll = func(string, string) (string, error) {
		return `{"access_token":"gho_late"}`, nil
	}
	a.pollDeviceFlow(gen)
	if a.syncer.device != nil || a.syncer.cfg.Token == "gho_late" {
		t.Error("the cancelled flow took a late answer")
	}
}

// A start that can't reach GitHub says so instead of showing a code.
func TestDeviceFlowStartFails(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	was := deviceStart
	defer func() { deviceStart = was }()
	deviceStart = func(string, string) (string, error) {
		return "", errors.New("no network")
	}
	a.startDeviceFlow()
	runPost(t, ch)
	if d := a.syncer.device; d == nil || d.err != "no network" {
		t.Fatalf("the failed start shows %+v", d)
	}
}
