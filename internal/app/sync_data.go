package app

import (
	"bytes"
	"encoding/json"
	"fmt"

	"mygo-music/internal/i18n"
	"mygo-music/internal/player"
	cloud "mygo-music/internal/sync"
)

// Player state belongs to the main thread. These adapters let network work
// snapshot and apply it there, preserving edits made while a request ran.
type appSyncData struct {
	app      *app
	playback bool
}

var deviceSettings = []string{"appChecked", "pluginsChecked", "pluginUpdates", "notchGreeted"}

func (s appSyncData) snapshot() (cloud.Document, error) {
	if s.playback {
		out := cloud.Document{}
		if s.app.pl != nil {
			if session, ok := s.app.pl.Session(); ok {
				// Queue order, current index and position form one indivisible value.
				data, err := json.Marshal(session)
				if err != nil {
					return nil, err
				}
				out["session"], err = json.Marshal(string(data))
				if err != nil {
					return nil, err
				}
			}
		}
		return out, nil
	}
	data, err := json.Marshal(s.app.settings)
	if err != nil {
		return nil, err
	}
	defaults, _ := json.Marshal(defaultSettings())
	var out, standard cloud.Document
	if err = json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(defaults, &standard); err != nil {
		return nil, err
	}
	for key, value := range out {
		if bytes.Equal(value, standard[key]) {
			delete(out, key)
		}
	}
	for _, key := range deviceSettings {
		delete(out, key)
	}
	if string(out["source"]) == `"local"` {
		delete(out, "source")
	}
	return out, nil
}

func (s appSyncData) SnapshotData() (cloud.Document, error) {
	type response struct {
		data cloud.Document
		err  error
	}
	result := make(chan response, 1)
	s.app.post(func() { data, err := s.snapshot(); result <- response{data, err} })
	r := <-result
	return r.data, r.err
}

func (s appSyncData) ApplyData(before, accepted cloud.Document) error {
	result := make(chan error, 1)
	s.app.post(func() { result <- s.apply(before, accepted) })
	return <-result
}

func (s appSyncData) apply(before, accepted cloud.Document) error {
	a := s.app
	current, err := s.snapshot()
	if err != nil {
		return err
	}
	merged, _, err := cloud.MergeDocument(before, current, accepted, true)
	if err != nil {
		return err
	}
	if s.playback {
		if a.pl == nil || a.pl.Playing() {
			return nil
		}
		if bytes.Equal(current["session"], merged["session"]) {
			return nil
		}
		var encoded string
		if err = json.Unmarshal(merged["session"], &encoded); err != nil {
			return err
		}
		var session player.Session
		if err = json.Unmarshal([]byte(encoded), &session); err != nil {
			return err
		}
		if len(session.Queue) != len(session.Order) || session.At < 0 || session.At >= len(session.Queue) {
			return fmt.Errorf("invalid synced playback queue")
		}
		seen := make(map[int]bool, len(session.Order))
		for _, index := range session.Order {
			if index < 0 || index >= len(session.Queue) || seen[index] {
				return fmt.Errorf("invalid synced playback order")
			}
			seen[index] = true
		}
		a.pl.Restore(session)
		if session.Radio != "" && a.pl.Current() != nil {
			a.pl.AttachRadio(session.Radio, a.radioFeed(session.Radio))
		}
		a.saveSession()
		return nil
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	next := defaultSettings()
	if err = json.Unmarshal(data, &next); err != nil {
		return err
	}
	next.path = a.settings.path
	next.AppChecked, next.PluginsChecked, next.PluginUpdates, next.NotchGreeted = a.settings.AppChecked, a.settings.PluginsChecked, a.settings.PluginUpdates, a.settings.NotchGreeted
	if err = next.save(); err != nil {
		return err
	}
	a.settings = next
	a.settings.applyAppearance()
	i18n.SetLanguage(i18n.Resolve(next.Language, systemLanguages()))
	a.volume = next.Volume
	if a.pl != nil {
		a.pl.SetVolume(next.Volume)
	}
	a.enabledChanged()
	targetID := next.Source
	if targetID == "" {
		targetID = "local"
	}
	if target := a.libOf(targetID); target != nil && a.library != target {
		a.switchSource(target.id)
	} else if a.library != nil && a.libOf(a.library.id) == nil && len(a.libs) > 0 {
		a.switchSource(a.libs[0].id)
	}
	a.redraw()
	return nil
}
