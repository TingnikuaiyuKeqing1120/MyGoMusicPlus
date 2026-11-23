package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	cloud "mygo-music/internal/sync"
)

// SyncOptions identifies cache/device-specific storage and settings while
// other data participates without provider-specific code.
type SyncOptions struct {
	LocalKeys     []string `json:"localKeys,omitempty"`
	LocalSettings []string `json:"localSettings,omitempty"`
}

func (p *Plugin) snapshotData() (cloud.Document, error) {
	storage := maps.Clone(p.storage)
	for _, key := range p.manifest.Sync.LocalKeys {
		delete(storage, key)
	}
	settingsCopy := maps.Clone(p.settings)
	for _, key := range p.manifest.Sync.LocalSettings {
		delete(settingsCopy, key)
	}
	settings, err := json.Marshal(settingsCopy)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(storage)
	if err != nil {
		return nil, err
	}
	return cloud.Document{"settings": settings, "storage": data}, nil
}

// FilterData applies the installed plugin's local-key declarations to earlier
// cloud documents too, so a newly excluded cache is removed from future writes.
func (p *Plugin) FilterData(data cloud.Document) (cloud.Document, error) {
	out := maps.Clone(data)
	if out == nil {
		out = cloud.Document{}
	}
	if raw, ok := out["settings"]; ok {
		var settings map[string]string
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, err
		}
		for _, key := range p.manifest.Sync.LocalSettings {
			delete(settings, key)
		}
		b, err := json.Marshal(settings)
		if err != nil {
			return nil, err
		}
		out["settings"] = b
	}
	if raw, ok := out["storage"]; ok {
		var storage map[string]json.RawMessage
		if err := json.Unmarshal(raw, &storage); err != nil {
			return nil, err
		}
		for _, key := range p.manifest.Sync.LocalKeys {
			delete(storage, key)
		}
		b, err := json.Marshal(storage)
		if err != nil {
			return nil, err
		}
		out["storage"] = b
	}
	return out, nil
}

func (p *Plugin) SnapshotData() (cloud.Document, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotData()
}

// ApplyData stops the old module so top-level cached values are loaded again.
// Its latest host writes are merged under the same mutex as storage.set.
func (p *Plugin) ApplyData(before, accepted cloud.Document) error {
	p.starting.Lock()
	defer p.starting.Unlock()
	p.mu.Lock()
	v := p.vm
	p.vm = nil
	p.mu.Unlock()
	if v != nil {
		v.close()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	current, err := p.snapshotData()
	if err != nil {
		return err
	}
	merged, _, err := cloud.MergeDocument(before, current, accepted, true)
	if err != nil {
		return err
	}
	settings := map[string]string{}
	storage := map[string]json.RawMessage{}
	if b, ok := merged["settings"]; ok {
		if err = json.Unmarshal(b, &settings); err != nil {
			return err
		}
	}
	if b, ok := merged["storage"]; ok {
		if err = json.Unmarshal(b, &storage); err != nil {
			return err
		}
	}
	if settings == nil {
		settings = map[string]string{}
	}
	if storage == nil {
		storage = map[string]json.RawMessage{}
	}
	for _, key := range p.manifest.Sync.LocalSettings {
		delete(settings, key)
		if value, ok := p.settings[key]; ok {
			settings[key] = value
		}
	}
	for _, key := range p.manifest.Sync.LocalKeys {
		delete(storage, key)
		if value, ok := p.storage[key]; ok {
			storage[key] = value
		}
	}
	if err = p.writeJSON("settings.json", settings); err != nil {
		return err
	}
	if err = p.writeJSON("storage.json", storage); err != nil {
		return errors.Join(err, p.writeJSON("settings.json", p.settings))
	}
	p.settings, p.storage = settings, storage
	return nil
}

// OnDataChange is called after a successful user-data write. Cache keys do not
// start syncs; app callbacks only enqueue work on the main thread.
func (p *Plugin) OnDataChange(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onDataChange = fn
}

func (p *Plugin) dataChanged(key string, before, after []byte) {
	p.mu.Lock()
	fn := p.onDataChange
	local := slices.Contains(p.manifest.Sync.LocalKeys, key)
	p.mu.Unlock()
	if fn != nil && !local && !bytes.Equal(before, after) {
		fn()
	}
}
