package app

import (
	"encoding/json"
	"log"
	"os"
	"slices"
	"time"

	"mygo-music/internal/i18n"
	"mygo-music/internal/sync"
)

// Sync keeps the listener's data the same across their devices through a
// private GitHub repository: what changes here is pushed soon after, and
// what changed elsewhere is pulled now and then, each file taken from
// whichever side wrote it last. The token stays on this device, never synced.

const (
	// syncEvery is how often the app looks for what changed elsewhere.
	syncEvery = 5 * time.Minute
	// syncDebounce is how long a change waits before it's pushed, so a few
	// changes together go once.
	syncDebounce = 3 * time.Second
)

// syncState is what's going on with syncing.
type syncState struct {
	cfg   sync.Config
	saved sync.Config // as last kept, for the fields to save on a change
	busy  bool
	err   string
	last  time.Time // when syncing last came through
	dirty bool      // a change here awaits its push
}

// saveSyncConfig keeps the choices on this device.
func (a *app) saveSyncConfig() {
	writeSyncConfig(a.syncer.cfg)
	a.syncer.saved = a.syncer.cfg
}

// syncConfigPath is where the token and the choices stay, on this device
// alone: not among what syncs.
func syncConfigPath() string { return userDataDir("sync.json") }

func loadSyncConfig() sync.Config {
	c := sync.Config{Repo: "mygo-music-sync", Units: map[sync.Unit]bool{}}
	if b, err := os.ReadFile(syncConfigPath()); err == nil {
		json.Unmarshal(b, &c)
	}
	if c.Units == nil {
		c.Units = map[sync.Unit]bool{}
	}
	return c
}

func writeSyncConfig(c sync.Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(syncConfigPath(), b, 0o600)
}

// keepSyncing looks for what changed elsewhere now and then, for as long as
// the app runs, syncing only while it's set up and a kind is on.
func (a *app) keepSyncing() {
	if a.syncer.cfg.Enabled() {
		a.syncNow()
	}
	time.AfterFunc(syncEvery, func() { a.post(a.keepSyncing) })
}

// syncNow pulls what changed elsewhere, then pushes what changed here.
func (a *app) syncNow() {
	s := &a.syncer
	if !s.cfg.Enabled() || s.busy {
		return
	}
	s.busy, s.err = true, ""
	cfg := s.cfg
	a.redraw()
	go func() {
		e := &sync.Engine{Root: userDataDir(""), Store: sync.NewGitHub(cfg.Token, cfg.Repo)}
		err := e.Pull(cfg.Units)
		if err == nil {
			err = e.Push(cfg.Units)
		}
		a.post(func() {
			s.busy = false
			if err != nil {
				s.err = err.Error()
				a.redraw()
				return
			}
			s.last, s.dirty = time.Now(), false
			a.syncApplied()
		})
	}()
}

// markSyncDirty notes a change here, to push once a few have gathered.
func (a *app) markSyncDirty() {
	s := &a.syncer
	if !s.cfg.Enabled() {
		return
	}
	s.dirty = true
	time.AfterFunc(syncDebounce, func() {
		a.post(func() {
			if s.dirty {
				a.syncNow()
			}
		})
	})
}

// syncApplied takes in what a pull brought: the settings as they now are,
// and any plugin that arrived, live. A plugin that went elsewhere stays here
// until removed by hand, so nothing is lost to a sync.
func (a *app) syncApplied() {
	a.settings = loadSettings()
	a.settings.applyAppearance()
	i18n.SetLanguage(i18n.Resolve(a.settings.Language, systemLanguages()))
	a.volume = a.settings.Volume
	if a.pl != nil {
		a.pl.SetVolume(a.volume)
	}
	entries, _ := os.ReadDir(pluginsDir())
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		id := ent.Name()
		if slices.ContainsFunc(a.allLibs, func(l *library) bool { return l.id == id }) {
			continue
		}
		if err := a.pluginInstalled(id); err != nil {
			log.Printf("sync: taking in plugin %s: %v", id, err)
		}
	}
	a.redraw()
}