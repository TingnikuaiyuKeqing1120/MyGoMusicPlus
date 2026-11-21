package app

import (
	"encoding/json"
	"errors"

	"maps"
	"net/http"
	"os"
	"path/filepath"
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

var newSyncRepository = sync.NewGitHub

// syncState is what's going on with syncing.
type syncState struct {
	cfg           sync.Config
	saved         sync.Config // last submitted values, to detect edits in the form
	saving        bool
	configPending bool // latest credentials still need a durable write
	summary       string
	phase         string
	done, total   int
	busy          bool
	authFailed    bool
	err           string
	last          time.Time // when syncing last came through
	dirty         bool      // a change here awaits its push
	// device is a GitHub sign-in on its way, the device flow's code showing
	// while the listener enters it; deviceGen counts the ones started, to
	// tell the last from one put away.
	device    *deviceFlow
	deviceGen int
}

// saveSyncConfig keeps the choices on this device.
func (a *app) saveSyncConfig(done func(error)) {
	s := &a.syncer
	if s.saving {
		return
	}
	cfg := s.cfg
	cfg.Units = maps.Clone(cfg.Units)
	tokenChanged := cfg.Token != s.saved.Token
	s.saved, s.saving, s.configPending = cfg, true, true
	go func() {
		err := writeSyncConfig(cfg)
		a.post(func() {
			s.saving, s.configPending = false, err != nil
			if err != nil {
				s.err = Tf("Couldn't save sync settings (%s). Try syncing again.", err.Error())
			} else {
				s.err = ""
				if tokenChanged {
					s.authFailed = false
				}
			}
			if done != nil {
				done(err)
			}
		})
	}()
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
	path := syncConfigPath()
	f, err := os.CreateTemp(filepath.Dir(path), ".sync-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	// Web writes and renames reach OPFS asynchronously. A successful save
	// includes the destination's write-back, so a restart keeps the token pair.
	f, err = os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

// keepSyncing looks for what changed elsewhere now and then, for as long as
// the app runs, syncing only while it's set up and a kind is on.
func (a *app) keepSyncing() {
	if a.syncer.cfg.Enabled() && !a.syncer.authFailed {
		a.syncNow()
	}
	time.AfterFunc(syncEvery, func() { a.post(a.keepSyncing) })
}

// syncNow pulls what changed elsewhere, then pushes what changed here.
func (a *app) syncNow() {
	s := &a.syncer
	if !s.cfg.Enabled() || s.busy || s.saving || s.device != nil {
		return
	}
	s.busy, s.dirty, s.err, s.summary = true, false, "", ""
	s.phase, s.done, s.total = "Connecting to GitHub…", 0, 0
	cfg := s.cfg
	cfg.Units = maps.Clone(cfg.Units)
	root := userDataDir("")
	store := newSyncRepository(cfg.Token, cfg.Repo)
	store.Authorize = a.syncAuthorization(cfg)
	progress := func(phase string, done, total int) { a.post(func() { s.phase, s.done, s.total = phase, done, total }) }
	a.redraw()
	go func() {
		files := &sync.Engine{Root: root, Store: store, Progress: progress}
		err := store.EnsureRepo()
		if err == nil && cfg.Units[sync.UnitPlugins] {
			files.RepoID = store.RepositoryID()
			for attempt := 0; attempt < 3; attempt++ {
				err = files.SyncFiles(map[sync.Unit]bool{sync.UnitPlugins: true})
				if !errors.Is(err, sync.ErrConflict) {
					break
				}
			}
		}
		var loaded []*plugin.Plugin
		if err == nil && cfg.Units[sync.UnitPlugins] {
			loaded = plugin.Discover(pluginsDir(), userDataDir("plugin-data"))
		}
		a.post(func() {
			if err != nil {
				a.endSync(err)
				return
			}
			a.syncApplied(files.Changed, loaded)
			a.syncDocuments(root, store, cfg, progress)
		})
	}()
}

func (a *app) syncDocuments(root string, store *sync.GitHub, cfg sync.Config, progress func(string, int, int)) {
	type documentJob struct {
		name, title, id string
		source          sync.DocumentSource
	}
	var jobs []documentJob
	if cfg.Units[sync.UnitSettings] {
		jobs = append(jobs, documentJob{"preferences", T("Settings"), "", appSyncData{app: a}})
	}
	if cfg.Units[sync.UnitPlugins] {
		for _, p := range a.plugins {
			info := p.Info()
			jobs = append(jobs, documentJob{"plugin-" + info.ID, info.Name, info.ID, p})
		}
	}
	if cfg.Units[sync.UnitSettings] {
		jobs = append(jobs, documentJob{"playback", T("Playback"), "", appSyncData{app: a, playback: true}})
	}
	local := a.local
	go func() {
		var summaries []string
		conflicts := 0
		total := len(jobs)
		if cfg.Units[sync.UnitPlaylists] && local != nil {
			total++
		}
		for i, job := range jobs {
			progress("Syncing saved data", i, total)
			result, err := sync.SyncDocument(store, root, store.RepositoryID(), job.name, job.source)
			if err != nil {
				a.post(func() { a.endSync(err) })
				return
			}
			conflicts += result.Conflicts
			summaries = append(summaries, Tf("%s synced", job.title))
			if result.Changed && job.id != "" {
				a.post(func() {
					if lib := a.libOf(job.id); lib != nil {
						lib.liked = nil
						a.loadAccount(lib)
						a.libraryChanged(lib)
					}
				})
			}
		}
		if cfg.Units[sync.UnitPlaylists] && local != nil {
			progress("Merging playlists and likes", len(jobs), total)
			result, err := sync.SyncCollection(store, root, store.RepositoryID(), "local", local)
			if err != nil {
				a.post(func() { a.endSync(err) })
				return
			}
			summaries = append(summaries, Tf("%s: %d playlists, %d likes", T("Local Files"), result.Playlists, result.Likes))
			if result.Changed {
				a.post(func() {
					if lib := a.libOf("local"); lib != nil {
						lib.liked = nil
						a.loadAccount(lib)
						a.libraryChanged(lib)
					}
				})
			}
		}
		if conflicts > 0 {
			summaries = append(summaries, Tf("%d conflicting edits backed up", conflicts))
		}
		a.post(func() { a.syncer.done = total; a.syncer.summary = strings.Join(summaries, " · "); a.endSync(nil) })
	}()
}

func (a *app) endSync(err error) {
	s := &a.syncer
	s.busy = false
	s.authFailed = false
	if err != nil {
		s.err = err.Error()
		var githubErr *sync.GitHubError
		if errors.Is(err, errSyncReauthorization) || errors.As(err, &githubErr) && githubErr.StatusCode == http.StatusUnauthorized {
			s.authFailed = true
			s.err = T("GitHub sign-in could not be renewed. Sign in with GitHub again, then sync.")
			a.toast = T("GitHub sign-in is invalid. Open Settings → Sync to sign in again.")
		}
		return
	}
	s.last = time.Now()
	if s.dirty {
		a.markSyncDirty()
	}
}

// markSyncDirty notes a change here, to push once a few have gathered.
func (a *app) markSyncDirty() {
	s := &a.syncer
	if !s.cfg.Enabled() || s.authFailed {
		return
	}
	s.dirty = true
	time.AfterFunc(syncDebounce, func() {
		a.post(func() {
			if s.dirty && !s.authFailed {
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
