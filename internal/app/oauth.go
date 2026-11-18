package app

import (
	"encoding/json"
	"time"
)

// githubClientID is the app's OAuth application on GitHub, for signing in to
// sync without writing a token by hand. A client ID of the device flow is
// public by design; no secret goes with it.
const githubClientID = "Ov23lirb1mU9F61UuSC1"

// deviceStart and devicePoll carry out the device flow's two requests. The
// shell does the HTTP, GitHub's sign-in endpoints not being open to a
// browser's fetch; they stay nil where no shell offers it, as in a plain
// browser, where the token is written by hand. Tests replace them.
var (
	deviceStart func(clientID, scope string) (string, error)
	devicePoll  func(clientID, deviceCode string) (string, error)
)

// openGitHubDevicePage lets the Android shell choose GitHub Mobile or a browser.
var openGitHubDevicePage func(preferApp bool) bool

var copyDeviceCode = func(code string) error { return copyText(code) }

// deviceFlow is a GitHub sign-in on its way: the code the listener enters at
// GitHub's device page, and the asking for the token once they have.
type deviceFlow struct {
	copyStatus string
	userCode   string
	uri        string
	code       string // the device code, for the polling
	interval   time.Duration
	expires    time.Time
	err        string
}

// startDeviceFlow asks GitHub for a code to sign in with, the shell making
// the request. What came of it shows on the sync page.
func (a *app) startDeviceFlow() {
	s := &a.syncer
	s.deviceGen++
	gen := s.deviceGen
	s.device = &deviceFlow{}
	go func() {
		body, err := deviceStart(githubClientID, "repo")
		a.post(func() {
			if s.deviceGen != gen || s.device == nil {
				return
			}
			d := s.device
			if err != nil {
				d.err = err.Error()
				return
			}
			var res struct {
				DeviceCode string `json:"device_code"`
				UserCode   string `json:"user_code"`
				URI        string `json:"verification_uri"`
				ExpiresIn  int    `json:"expires_in"`
				Interval   int    `json:"interval"`
				Error      string `json:"error_description"`
			}
			if json.Unmarshal([]byte(body), &res); res.DeviceCode == "" {
				d.err = res.Error
				if d.err == "" {
					d.err = T("GitHub sign-in didn't start. Try again.")
				}
				return
			}
			d.code, d.userCode, d.uri = res.DeviceCode, res.UserCode, res.URI
			d.interval = time.Duration(res.Interval) * time.Second
			if d.interval < time.Millisecond {
				d.interval = 5 * time.Second
			}
			d.expires = time.Now().Add(time.Duration(res.ExpiresIn) * time.Second)
			a.scheduleDevicePoll(gen, d.interval)
		})
	}()
}

// cancelDeviceFlow puts away a sign-in on its way, its polling going with it.
func (a *app) cancelDeviceFlow() {
	a.syncer.deviceGen++
	a.syncer.device = nil
}

func (a *app) scheduleDevicePoll(gen int, after time.Duration) {
	time.AfterFunc(after, func() { a.post(func() { a.pollDeviceFlow(gen) }) })
}

// pollDeviceFlow asks once whether the listener has signed in yet, and keeps
// asking while GitHub says they haven't, until the token comes or the code
// expires or they refuse.
func (a *app) pollDeviceFlow(gen int) {
	s := &a.syncer
	d := s.device
	if d == nil || s.deviceGen != gen || d.err != "" {
		return
	}
	if time.Now().After(d.expires) {
		d.err = T("The sign-in code expired. Start again.")
		return
	}
	code := d.code
	go func() {
		body, err := devicePoll(githubClientID, code)
		a.post(func() {
			if s.device == nil || s.deviceGen != gen {
				return
			}
			d := s.device
			if err != nil {
				d.err = err.Error()
				return
			}
			var res struct {
				Token string `json:"access_token"`
				Error string `json:"error"`
			}
			json.Unmarshal([]byte(body), &res)
			switch {
			case res.Token != "":
				s.cfg.Token = res.Token
				a.saveSyncConfig()
				s.device = nil
				a.toast = T("Signed in to GitHub")
			case res.Error == "authorization_pending":
				a.scheduleDevicePoll(gen, d.interval)
			case res.Error == "slow_down":
				d.interval += 5 * time.Second
				a.scheduleDevicePoll(gen, d.interval)
			case res.Error == "expired_token":
				d.err = T("The sign-in code expired. Start again.")
			case res.Error == "access_denied":
				d.err = T("The sign-in was cancelled on GitHub.")
			default:
				d.err = T("GitHub sign-in didn't come through. Try again.")
			}
		})
	}()
}
