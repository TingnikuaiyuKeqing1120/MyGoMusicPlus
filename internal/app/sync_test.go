package app

import (
	"testing"

	"mygo-music/internal/sync"
)

// The token and choices stay on this device, and come back as they were kept.
func TestSyncConfigRoundTrip(t *testing.T) {
	c := sync.Config{Token: "ghp_secret", Repo: "my-sync", Units: map[sync.Unit]bool{sync.UnitSettings: true}}
	if err := writeSyncConfig(c); err != nil {
		t.Fatal(err)
	}
	got := loadSyncConfig()
	if got.Token != "ghp_secret" || got.Repo != "my-sync" || !got.Units[sync.UnitSettings] {
		t.Fatalf("the config didn't come back: %+v", got)
	}
	if !got.Enabled() {
		t.Fatal("a token, a repo and a kind on should enable syncing")
	}
	// sync.json, which holds the token, sits outside every unit's paths, so
	// the engine never gathers it.
}

// Nothing syncs without a token, a repo, or a kind turned on.
func TestSyncEnabledGating(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	a.syncer.cfg = sync.Config{Repo: "r", Units: map[sync.Unit]bool{sync.UnitSettings: true}} // no token
	a.syncNow()
	if a.syncer.busy {
		t.Fatal("syncing started without a token")
	}
	a.markSyncDirty()
	if a.syncer.dirty {
		t.Fatal("a change was marked while syncing is off")
	}
	select {
	case fn := <-ch:
		fn()
		t.Fatal("something was posted while syncing is off")
	default:
	}
}