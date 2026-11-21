package player

import (
	"errors"
	"slices"
	"time"

	"mygo-music/internal/music"
)

// nativePlayback lets an Android service own decoding, output and the ordered
// queue. Stream resolution remains with the source that owns each track.
type nativePlayback interface {
	command(nativeCommand)
	resolve(generation, slot int, info *music.Stream, err error)
	cache(action string, limit int64) nativeCacheStatus
}

type nativeItem struct {
	Slot     int         `json:"slot"`
	Track    music.Track `json:"track"`
	CacheKey string      `json:"cacheKey"`
}

type nativeCommand struct {
	Action     string       `json:"action"`
	Generation int          `json:"generation"`
	Items      []nativeItem `json:"items,omitempty"`
	Slot       int          `json:"slot"`
	Position   int64        `json:"position,omitempty"`
	Play       bool         `json:"play"`
	Repeat     RepeatMode   `json:"repeat"`
	Volume     float64      `json:"volume"`
	Normalize  bool         `json:"normalize"`
}

type nativeState struct {
	Generation int                `json:"generation"`
	Slot       int                `json:"slot"`
	Position   int64              `json:"position"`
	Duration   int64              `json:"duration"`
	WantPlay   bool               `json:"wantPlay"`
	Playing    bool               `json:"playing"`
	Buffering  bool               `json:"buffering"`
	Ready      bool               `json:"ready"`
	Ended      bool               `json:"ended"`
	Error      string             `json:"error"`
	Buffered   float64            `json:"buffered"`
	Bands      [Bands]float32     `json:"bands"`
	Cache      *nativeCacheStatus `json:"cache,omitempty"`
	Stream     *music.Stream      `json:"stream,omitempty"`
}

type nativeCacheStatus struct {
	Bytes int64    `json:"bytes"`
	Keys  []string `json:"keys"`
}

// NativeCache configures or measures Android's stream cache. The returned keys
// are posted back to the player thread for list indicators.
func (p *Player) NativeCache(action string, limit int64) (int64, int, bool) {
	if p.native == nil {
		return 0, 0, false
	}
	s := p.native.cache(action, limit)
	p.post(func() { p.setNativeCache(s) })
	return s.Bytes, len(s.Keys), true
}

func (p *Player) setNativeCache(s nativeCacheStatus) {
	p.nativeCached = make(map[string]bool, len(s.Keys))
	for _, key := range s.Keys {
		p.nativeCached[key] = true
	}
}

// Cached includes audio held by the Android service as well as existing Go files.
func (p *Player) Cached(source, id, quality string) bool {
	if p.Cache.Cached(source, id, quality) {
		return true
	}
	at := max(slices.Index(music.Qualities, quality), 0)
	for _, q := range music.Qualities[at:] {
		if p.nativeCached[cacheKey(source, id, q)] {
			return true
		}
	}
	return false
}

var makeNativePlayback func(state func(nativeState), resolve func(int, int)) nativePlayback

func (p *Player) nativeCommand(action string) {
	if !p.nativeActive {
		return
	}
	slot := 0
	if p.at >= 0 && p.at < len(p.order) {
		slot = p.order[p.at]
	}
	c := nativeCommand{Action: action, Generation: p.gen, Slot: slot, Play: p.playing,
		Repeat: p.repeat, Volume: p.volume * p.volume, Normalize: p.Normalize != nil && p.Normalize()}
	if action == "queue" {
		for _, slot := range p.order {
			t := p.queue[slot]
			c.Items = append(c.Items, nativeItem{Slot: slot, Track: t, CacheKey: cacheKey(t.Source, t.ID, p.nativeQuality)})
		}
		c.Position = p.pending.Milliseconds()
	}
	p.native.command(c)
}

func (p *Player) nativeResolve(generation, slot int) {
	if !p.nativeActive || generation != p.gen || slot < 0 || slot >= len(p.queue) {
		return
	}
	t := p.queue[slot]
	src := p.sourceOf(&t)
	quality := p.nativeQuality
	go func() {
		var info *music.Stream
		var err error
		if file, stored, ok := p.Cache.find(t.Source, t.ID, quality); ok {
			copy := *stored
			copy.URL = music.FileURL(file.Name())
			file.Close()
			info = &copy
		} else if src == nil {
			err = errNoSource
		} else {
			info, err = src.Stream(t.ID, quality)
		}
		if err == nil && (info == nil || info.URL == "") {
			err = music.ErrUnavailable
		}
		p.post(func() {
			if !p.nativeActive || generation != p.gen {
				return
			}
			if err == nil {
				delete(p.nativeErrors, slot)
				p.nativeStreams[slot] = info
				if p.order[p.at] == slot {
					p.info = info
				}
			} else {
				p.nativeErrors[slot] = err
			}
			p.native.resolve(generation, slot, info, err)
		})
	}()
}

func (p *Player) nativeChanged(s nativeState) {
	if !p.nativeActive || s.Generation != p.gen || s.Slot < 0 || s.Slot >= len(p.queue) {
		return
	}
	if s.Cache != nil {
		p.setNativeCache(*s.Cache)
	}
	at := -1
	for k, slot := range p.order {
		if slot == s.Slot {
			at = k
			break
		}
	}
	if at < 0 {
		return
	}
	if p.nativeFailed && s.Error != "" && p.at == at {
		return
	}
	changed := p.at != at
	wasReady := p.nativeState.Ready
	p.nativeState, p.nativeAt = s, p.now()
	p.loading, p.playing, p.pending = s.Buffering, s.WantPlay, 0
	if s.Stream != nil {
		p.info = s.Stream
		p.nativeStreams[s.Slot] = s.Stream
	}
	if changed {
		t := p.queue[s.Slot]
		if src := p.sourceOf(&t); src != nil && src.Info().Playback == music.PlaybackRemote {
			p.loadSkipping(at, 0, forward, p.playing, time.Duration(s.Position)*time.Millisecond)
			return
		}
		p.at = at
		p.nativeDirection, p.nativeSkipped = forward, 0
		if p.nativeStep != 0 {
			p.nativeDirection = p.nativeStep
			p.nativeStep = 0
		}
		p.nativeFailed = false
		p.track, p.info, p.recorded = &t, p.nativeStreams[s.Slot], ""
		p.lyrics, p.lyricsLoaded = nil, false
		p.loadCover(p.gen, &t)
		p.loadLyrics(p.gen, p.sourceOf(&t), t)
		p.refill()
	}
	if s.Error != "" {
		if !p.nativeFailed {
			p.nativeFailed = true
			err := p.nativeErrors[s.Slot]
			if err == nil {
				err = errors.New(s.Error)
			}
			p.loadFailed(p.track.Source, err, at, p.nativeSkipped, p.nativeDirection)
			if !p.playing {
				p.nativeCommand("play")
			}
		}
		return
	}
	p.nativeFailed = false
	p.err = ""
	if p.playing && s.Ready && !s.Buffering {
		p.record()
	}
	if s.Ended && p.feed != nil && !p.awaiting {
		p.awaitFeed()
	}
	// Playback state belongs to the service. Changed persists the session and
	// updates caption state without sending the state back through MediaSession.
	if p.Changed != nil && (changed || !wasReady && s.Ready || s.Ended || p.nativeWasPlaying != p.playing) {
		p.Changed()
	}
	p.nativeWasPlaying = p.playing
}

func (p *Player) nativePosition() time.Duration {
	pos := time.Duration(p.nativeState.Position) * time.Millisecond
	if p.nativeState.Playing {
		pos += p.now().Sub(p.nativeAt)
	}
	if p.nativeState.Duration > 0 {
		pos = min(pos, time.Duration(p.nativeState.Duration)*time.Millisecond)
	}
	return pos
}
