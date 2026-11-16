package app

import (
	"path/filepath"

	"github.com/egoist/mygo"

	"mygo-music/internal/music"
	"mygo-music/internal/player"
)

// newSongCache returns the cache of songs played, in the app's cache
// directory, or nil without one.
func newSongCache(mb int) *player.Cache {
	dir, err := mygo.App.Path(mygo.PathCache)
	if err != nil {
		return nil
	}
	return player.NewCache(filepath.Join(dir, "songs"), int64(mb)<<20)
}

// applyCacheLimit gives the cache the limit the settings say.
func (a *app) applyCacheLimit() {
	if cache := a.pl.Cache; cache != nil {
		limit := int64(a.settings.CacheMB) << 20
		go func() {
			cache.SetLimit(limit)
			a.measureCache()
		}()
	}
}

// measureCache finds how much the cache takes, on a goroutine, for the
// settings to show.
func (a *app) measureCache() {
	cache := a.pl.Cache
	if cache == nil {
		return
	}
	go func() {
		cache.Refresh()
		n := cache.Size()
		count := int64(cache.Count())
		a.post(func() {
			a.cacheUsed = n
			a.cacheCount = count
			if a.settingsWin != nil {
				a.settingsWin.Invalidate()
			}
		})
	}()
}

func (a *app) clearCache() {
	if cache := a.pl.Cache; cache != nil {
		a.cacheUsed = 0
		a.cacheCount = 0
		go func() {
			cache.Clear()
			a.measureCache()
		}()
	}
}

// isOffline reports whether tr is kept in the song cache, to play without the
// network, at the quality the settings say or a better one.
func (a *app) isOffline(tr *music.Track) bool {
	return a.pl.Cache.Cached(tr.Source, tr.ID, a.settings.Quality)
}

// cacheUsage says how many songs the cache keeps and how much they take, once
// measured.
func (a *app) cacheUsage() string {
	if a.cacheUsed < 0 {
		return T("Measuring the space used…")
	}
	var size string
	if a.cacheUsed >= 1<<30 {
		size = Tf("%.1f GB used", float64(a.cacheUsed)/(1<<30))
	} else {
		size = Tf("%d MB used", a.cacheUsed>>20)
	}
	return Tf("%d songs · %s", a.cacheCount, size)
}
