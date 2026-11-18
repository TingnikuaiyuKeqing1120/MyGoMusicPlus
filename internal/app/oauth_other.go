//go:build !js

package app

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The desktop can reach GitHub's login endpoints directly. The browser build
// uses the Android bridge where available.
func useDeviceFlow() {
	client := &http.Client{Timeout: 30 * time.Second}
	post := func(endpoint string, form url.Values) (string, error) {
		req, err := http.NewRequest(http.MethodPost, "https://github.com/login/"+endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer res.Body.Close()
		body, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
		return string(body), err
	}
	deviceStart = func(clientID, scope string) (string, error) {
		return post("device/code", url.Values{"client_id": {clientID}, "scope": {scope}})
	}
	devicePoll = func(clientID, code string) (string, error) {
		return post("oauth/access_token", url.Values{
			"client_id":   {clientID},
			"device_code": {code},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		})
	}
}
