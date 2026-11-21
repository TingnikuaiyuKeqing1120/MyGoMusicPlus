//go:build !js

package app

import (
	"fmt"
	"net/http"
	"testing"
)

// Desktop renewal sends the device-flow refresh grant with correctly encoded credentials.
func TestDesktopOAuthRefreshRequest(t *testing.T) {
	start, poll, refresh, transport := deviceStart, devicePoll, deviceRefresh, http.DefaultTransport
	t.Cleanup(func() {
		deviceStart, devicePoll, deviceRefresh, http.DefaultTransport = start, poll, refresh, transport
	})
	requests := 0
	http.DefaultTransport = syncTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "POST" || r.URL.String() != "https://github.com/login/oauth/access_token" || r.Header.Get("Accept") != "application/json" {
			t.Error("wrong token endpoint or response format")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != githubClientID || r.Form.Get("refresh_token") != "fixture+&=token" || len(r.Form) != 3 {
			t.Error("refresh grant was not form encoded correctly")
		}
		fmt.Fprint(w, `{"access_token":"renewed","refresh_token":"rotated","expires_in":28800}`)
	})}
	useDeviceFlow()
	body, err := deviceRefresh(githubClientID, "fixture+&=token")
	if err != nil || requests != 1 || body != `{"access_token":"renewed","refresh_token":"rotated","expires_in":28800}` {
		t.Fatal("refresh response was lost")
	}
}
