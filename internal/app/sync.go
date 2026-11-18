package app

import (
	"encoding/json"
	"errors"

	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"mygo-music/internal/i18n"
	"mygo-music/internal/plugin"
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
	cfg         sync.Config
	saved       sync.Config // as last kept, for the fields to save on a change
	summary     string
	phase       string
	done, total int
	busy        bool
	err         string
	last        time.Time // when syncing last came through
	dirty       bool      // a change here awaits its push
	// device is a GitHub sign-in on its way, the device flow's code showing
	// while the listener enters it; deviceGen counts the ones started, to
	// tell the last from one put away.
	device    *deviceFlow
	deviceGen int
}

// saveSyncConfig keeps the choices on this device.
func (a *app) saveSyncConfig() {
	if err := writeSyncConfig(a.syncer.cfg); err != nil {
		a.syncer.err = err.Error()
		return
	}
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
	s.busy, s.dirty, s.err, s.summary = true, false, "", ""
	s.phase, s.done, s.total = "Connecting to GitHub…", 0, 0
	cfg := s.cfg
	cfg.Units = maps.Clone(cfg.Units)
	playlists := cfg.Units[sync.UnitPlaylists]
	delete(cfg.Units, sync.UnitPlaylists)
	root := userDataDir("")
	store := sync.NewGitHub(cfg.Token, cfg.Repo)
	a.redraw()
	progress := func(phase string, done, total int) { a.post(func() { s.phase, s.done, s.total = phase, done, total }) }
	go func() {
		defaults, _ := json.Marshal(defaultSettings())
		files := &sync.Engine{Root: root, Store: store, Progress: progress, Defaults: defaults}
		err := store.EnsureRepo()
		if err == nil && cfg.Enabled() {
			files.RepoID = store.RepositoryID()
			for attempt := 0; attempt < 3; attempt++ {
				err = files.SyncFiles(cfg.Units)
				if !errors.Is(err, sync.ErrConflict) {
					break
				}
			}
		}
		var plugins []*plugin.Plugin
		if err == nil && slices.ContainsFunc(files.Changed, func(rel string) bool {
			return strings.HasPrefix(rel, "plugins/") || strings.HasPrefix(rel, "plugin-data/")
		}) {
			plugins = plugin.Discover(pluginsDir(), userDataDir("plugin-data"))
		}
		a.post(func() {
			if err != nil {
				a.endSync(err)
				return
			}
			if cfg.Enabled() {
				a.syncApplied(files.Changed, plugins)
			}
			if !playlists {
				a.endSync(nil)
				return
			}
			a.syncPlaylists(root, store, progress)
		})
	}()
}

func (a *app) syncPlaylists(root string, store *sync.GitHub, progress func(string, int, int)) {
	type collectionJob struct {
		id     string
		source sync.CollectionSource
	}
	var jobs []collectionJob
	if a.local != nil {
		jobs = append(jobs, collectionJob{"local", a.local})
	}
	for _, p := range a.plugins {
		if p.Info().ID == bundledGDID {
			jobs = append(jobs, collectionJob{bundledGDID, plugin.GDCollection{Player: p}})
			break
		}
	}
	go func() {
		var counts []string
		for i, job := range jobs {
			progress("Merging playlists and likes", i, len(jobs))
			result, err := sync.SyncCollection(store, root, store.RepositoryID(), job.id, job.source)
			if err != nil {
				a.post(func() { a.endSync(err) })
				return
			}
			title := T("Local Files")
			if job.id == bundledGDID {
				title = T("GD Music")
			}
			counts = append(counts, Tf("%s: %d playlists, %d likes", title, result.Playlists, result.Likes))
			a.post(func() {
				if lib := a.libOf(job.id); result.Changed && lib != nil {
					lib.liked = nil
					a.libraryChanged(lib)
				}
			})
		}
		if !slices.ContainsFunc(jobs, func(j collectionJob) bool { return j.id == bundledGDID }) {
			counts = append(counts, T("Install GD Music to sync its playlists"))
		}
		a.post(func() { a.syncer.summary = strings.Join(counts, " · "); a.endSync(nil) })
	}()
}

func (a *app) endSync(err error) {
	s := &a.syncer
	s.busy = false
	if err != nil {
		s.err = err.Error()
		return
	}
	s.last = time.Now()
	if s.dirty {
		a.syncNow()
	}
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
func (a *app) syncApplied(changed []string, plugins []*plugin.Plugin) {
	if slices.Contains(changed, "settings.json") {
		a.settings = loadSettings()
		a.settings.applyAppearance()
		i18n.SetLanguage(i18n.Resolve(a.settings.Language, systemLanguages()))
		a.volume = a.settings.Volume
		if a.pl != nil {
			a.pl.SetVolume(a.volume)
		}
	}
	for _, p := range plugins {
		id := p.Info().ID
		if slices.ContainsFunc(a.allLibs, func(l *library) bool { return l.id == id }) && !slices.ContainsFunc(changed, func(rel string) bool {
			return strings.HasPrefix(rel, "plugins/"+p.InstallFolder()+"/") || strings.HasPrefix(rel, "plugin-data/"+id+"/")
		}) {
			continue
		}
		a.activateInstalledPlugin(p)
	}
	a.redraw()
}
