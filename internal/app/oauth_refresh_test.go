package app

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	cloud "mygo-music/internal/sync"
)

// Expiration metadata follows the issued pair, and a long-lived sign-in clears the previous pair's metadata.
func TestOAuthTokenMetadata(t *testing.T) {
	now := time.Unix(1800000000, 0)
	res := oauthTokenResponse{Token: "access", RefreshToken: "refresh", ExpiresIn: 28800, RefreshTokenExpiresIn: 15897600}
	c := res.config(cloud.Config{Repo: "sync"}, now)
	if c.TokenExpiresAt != now.Unix()+28800 || c.RefreshTokenExpiresAt != now.Unix()+15897600 || c.Repo != "sync" {
		t.Fatal("wrong token expiration or lost repository")
	}
	c = (oauthTokenResponse{Token: "long-lived"}).config(c, now)
	if c.Token != "long-lived" || c.RefreshToken != "" || c.TokenExpiresAt != 0 || c.RefreshTokenExpiresAt != 0 {
		t.Fatal("long-lived sign-in retained a previous account's refresh token")
	}
}

// Sync renews expired credentials before requests, recovers one 401, and preserves transient failures for retry.
func TestSyncTokenRenewal(t *testing.T) {
	for _, tc := range []struct {
		name           string
		expires        int64
		refreshExpires int64
		refresh        string
		response       string
		network        bool
		firstStatus    int
		wantRefresh    int
		wantAuth       bool
		wantError      bool
	}{
		{name: "expired", expires: 1, refresh: "refresh-old", wantRefresh: 1},
		{name: "valid", expires: 4102444800, refresh: "refresh-old"},
		{name: "rejected", expires: 4102444800, refresh: "refresh-old", firstStatus: 401, wantRefresh: 1},
		{name: "unknown-expiry", refresh: "refresh-old", firstStatus: 401, wantRefresh: 1},
		{name: "legacy-valid"},
		{name: "legacy-rejected", firstStatus: 401, wantAuth: true, wantError: true},
		{name: "expired-refresh", expires: 1, refresh: "refresh-old", refreshExpires: 1, wantAuth: true, wantError: true},
		{name: "revoked-refresh", expires: 1, refresh: "refresh-old", response: `{"error":"bad_refresh_token"}`, wantRefresh: 1, wantAuth: true, wantError: true},
		{name: "network", expires: 1, refresh: "refresh-old", network: true, wantRefresh: 1, wantError: true},
		{name: "bridge-network", expires: 1, refresh: "refresh-old", response: `{"error":"network"}`, wantRefresh: 1, wantError: true},
		{name: "malformed", expires: 1, refresh: "refresh-old", response: `<html>unavailable</html>`, wantRefresh: 1, wantError: true},
		{name: "rate-limit", expires: 4102444800, refresh: "refresh-old", firstStatus: 403, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := userDataDir("")
			mygo.App.SetPath(mygo.PathUserData, t.TempDir())
			previousRefresh, previousRepository := deviceRefresh, newSyncRepository
			t.Cleanup(func() {
				mygo.App.SetPath(mygo.PathUserData, root)
				deviceRefresh, newSyncRepository = previousRefresh, previousRepository
			})
			refreshes, requests := 0, 0
			deviceRefresh = func(clientID, refreshToken string) (string, error) {
				refreshes++
				if clientID != githubClientID || refreshToken != "refresh-old" {
					t.Error("wrong refresh credentials")
				}
				if tc.network {
					return "", errors.New("offline")
				}
				if tc.response != "" {
					return tc.response, nil
				}
				return `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":28800,"refresh_token_expires_in":15897600}`, nil
			}
			newSyncRepository = func(token, repo string) *cloud.GitHub {
				g := cloud.NewGitHub(token, repo)
				g.Client.Transport = syncTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if requests == 1 && tc.firstStatus != 0 {
						w.WriteHeader(tc.firstStatus)
						fmt.Fprint(w, `{"message":"fixture refusal"}`)
						return
					}
					want := "access-old"
					if refreshes > 0 {
						want = "access-new"
						if stored := loadSyncConfig(); stored.Token != want || stored.RefreshToken != "refresh-new" {
							t.Error("request ran before rotated credentials were persisted")
						}
					}
					if r.Header.Get("Authorization") != "Bearer "+want {
						t.Error("request used the wrong access token")
					}
					if r.URL.Path == "/user" {
						fmt.Fprint(w, `{"login":"fixture"}`)
					} else {
						fmt.Fprint(w, `{}`)
					}
				})}
				return g
			}
			a := newTestApp()
			ch := posts(a)
			a.syncer.cfg = cloud.Config{Token: "access-old", RefreshToken: tc.refresh, TokenExpiresAt: tc.expires, RefreshTokenExpiresAt: tc.refreshExpires, Repo: "sync", Units: map[cloud.Unit]bool{cloud.UnitPlaylists: true}}
			a.syncer.saved = a.syncer.cfg
			a.syncNow()
			a.syncNow() // Auto-sync and the button must share the same in-flight renewal.
			until(t, ch, func() bool { return !a.syncer.busy })
			if refreshes != tc.wantRefresh || a.syncer.authFailed != tc.wantAuth || (a.syncer.err != "") != tc.wantError {
				t.Fatalf("refreshes=%d authFailed=%v error=%q", refreshes, a.syncer.authFailed, a.syncer.err)
			}
			if !tc.wantError && a.syncer.last.IsZero() {
				t.Fatal("sync did not finish")
			}
			if tc.wantError && tc.expires == 1 && requests != 0 {
				t.Fatal("sync sent an expired access token after renewal failed")
			}
		})
	}
}

// Entering a personal token removes the old OAuth pair so a later sync cannot silently switch back to it.
func TestManualSyncTokenReplacesOAuth(t *testing.T) {
	root, wasWeb := userDataDir(""), onWeb
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	onWeb = true
	t.Cleanup(func() {
		mygo.App.SetPath(mygo.PathUserData, root)
		onWeb = wasWeb
	})
	a := newTestApp()
	ch := posts(a)
	a.settingsWeb, a.settingsPage = true, "sync"
	a.syncer.cfg = cloud.Config{Token: "oauth-access", RefreshToken: "oauth-refresh", TokenExpiresAt: 1, RefreshTokenExpiresAt: 4102444800, Repo: "sync"}
	a.syncer.saved = a.syncer.cfg
	a.syncer.authFailed = true
	tt := ui.NewTester(a.view, 390, 1100)
	settled(tt)
	clickSettingRole(t, tt, ui.RoleTextField, T("GitHub Token"))
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("ghp_manual")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	until(t, ch, func() bool { return !a.syncer.saving })
	stored := loadSyncConfig()
	if stored.Token != "ghp_manual" || stored.RefreshToken != "" || stored.TokenExpiresAt != 0 || stored.RefreshTokenExpiresAt != 0 || a.syncer.authFailed {
		t.Fatalf("manual token state cfg=%+v stored=%+v authFailed=%v saving=%v error=%q", a.syncer.cfg, stored, a.syncer.authFailed, a.syncer.saving, a.syncer.err)
	}
}

// After rotation succeeds but saving fails, retry persists the new pair without reusing the consumed refresh token.
func TestSyncRenewalSaveRetry(t *testing.T) {
	root := userDataDir("")
	mygo.App.SetPath(mygo.PathUserData, t.TempDir())
	previousRefresh, previousRepository := deviceRefresh, newSyncRepository
	t.Cleanup(func() {
		mygo.App.SetPath(mygo.PathUserData, root)
		deviceRefresh, newSyncRepository = previousRefresh, previousRepository
	})
	if err := os.Mkdir(syncConfigPath(), 0700); err != nil {
		t.Fatal(err)
	}
	refreshes, requests := 0, 0
	deviceRefresh = func(string, string) (string, error) {
		refreshes++
		return `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":28800}`, nil
	}
	newSyncRepository = func(token, repo string) *cloud.GitHub {
		g := cloud.NewGitHub(token, repo)
		g.Client.Transport = syncTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests++
			if r.Header.Get("Authorization") != "Bearer access-new" || loadSyncConfig().RefreshToken != "refresh-new" {
				t.Error("request preceded persistence or used the consumed pair")
			}
			fmt.Fprint(w, `{"login":"fixture"}`)
		})}
		return g
	}
	a := newTestApp()
	ch := posts(a)
	a.syncer.cfg = cloud.Config{Token: "access-old", RefreshToken: "refresh-old", TokenExpiresAt: 1, Repo: "sync", Units: map[cloud.Unit]bool{cloud.UnitPlaylists: true}}
	a.syncNow()
	until(t, ch, func() bool { return !a.syncer.busy })
	if refreshes != 1 || requests != 0 || !a.syncer.configPending || a.syncer.authFailed || a.syncer.cfg.RefreshToken != "refresh-new" {
		t.Fatal("failed save lost the rotated pair or continued sync")
	}
	if err := os.Remove(syncConfigPath()); err != nil {
		t.Fatal(err)
	}
	a.syncNow()
	until(t, ch, func() bool { return !a.syncer.busy })
	if refreshes != 1 || requests != 2 || a.syncer.configPending || a.syncer.err != "" || loadSyncConfig().RefreshToken != "refresh-new" {
		t.Fatal("retry failed to persist and use the already-rotated pair")
	}
}
