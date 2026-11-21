package app

import (
	"encoding/json"
	"errors"
	"time"

	cloud "mygo-music/internal/sync"
)

var errSyncReauthorization = errors.New("GitHub authorization needs renewal")

type oauthTokenResponse struct {
	Token                 string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Error                 string `json:"error"`
}

func (r oauthTokenResponse) config(c cloud.Config, now time.Time) cloud.Config {
	c.Token, c.RefreshToken = r.Token, r.RefreshToken
	c.TokenExpiresAt, c.RefreshTokenExpiresAt = 0, 0
	if r.ExpiresIn > 0 {
		c.TokenExpiresAt = now.Unix() + r.ExpiresIn
	}
	if r.RefreshToken != "" && r.RefreshTokenExpiresIn > 0 {
		c.RefreshTokenExpiresAt = now.Unix() + r.RefreshTokenExpiresIn
	}
	return c
}

// syncAuthorization runs on the sync worker. The app's busy flag serializes
// syncs and disables credential edits while a refresh token is being rotated.
func (a *app) syncAuthorization(cfg cloud.Config) func(bool) (string, error) {
	pending := a.syncer.configPending
	refresh := deviceRefresh
	save := func() error {
		err := writeSyncConfig(cfg)
		applied := make(chan struct{})
		a.post(func() {
			// Rotation has already invalidated the old pair. Keep the new one
			// in memory even on disk failure, and persist it before the next sync.
			a.syncer.cfg, a.syncer.saved = cfg, cfg
			a.syncer.configPending = err != nil
			close(applied)
		})
		<-applied
		pending = err != nil
		if err != nil {
			return errors.New(Tf("Couldn't save sync settings (%s). Try syncing again.", err.Error()))
		}
		return nil
	}
	return func(rejected bool) (string, error) {
		if pending {
			if err := save(); err != nil {
				return "", err
			}
		}
		if cfg.RefreshToken == "" || refresh == nil {
			return cfg.Token, nil
		}
		now := time.Now()
		if !rejected && (cfg.TokenExpiresAt == 0 || cfg.TokenExpiresAt > now.Add(time.Minute).Unix()) {
			return cfg.Token, nil
		}
		if cfg.RefreshTokenExpiresAt > 0 && cfg.RefreshTokenExpiresAt <= now.Unix() {
			return "", errSyncReauthorization
		}
		body, err := refresh(githubClientID, cfg.RefreshToken)
		if err != nil {
			return "", errors.New(T("GitHub sign-in renewal failed. Try syncing again."))
		}
		var res oauthTokenResponse
		if json.Unmarshal([]byte(body), &res) != nil {
			return "", errors.New(T("GitHub sign-in renewal failed. Try syncing again."))
		}
		switch res.Error {
		case "bad_refresh_token", "invalid_grant", "expired_token", "access_denied":
			return "", errSyncReauthorization
		case "":
			if res.Token != "" && res.RefreshToken != "" {
				cfg = res.config(cfg, time.Now())
				if err := save(); err != nil {
					return "", err
				}
				return cfg.Token, nil
			}
		}
		return "", errors.New(T("GitHub sign-in renewal failed. Try syncing again."))
	}
}
