package app

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/i18n"
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
