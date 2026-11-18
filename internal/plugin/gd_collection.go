package plugin

import (
	"encoding/json"
	"maps"
	"reflect"

	cloud "mygo-music/internal/sync"
)

// GDCollection maps GD Music's known storage keys to portable playlists.
// Other plugins own their own schemas, including private account state.
type GDCollection struct{ Player *Plugin }

type gdList struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Mine     bool     `json:"mine"`
	TrackIDs []string `json:"trackIds"`
}

func gdSnapshot(storage map[string]json.RawMessage) (cloud.Collection, map[string]json.RawMessage, error) {
	var c cloud.Collection
	var lists []gdList
	songs := map[string]json.RawMessage{}
	for k, v := range map[string]any{"liked": &c.Likes, "playlists": &lists, "songs": &songs} {
		if raw := storage[k]; len(raw) > 0 {
			if err := json.Unmarshal(raw, v); err != nil {
				return c, nil, err
			}
		}
	}
	for _, p := range lists {
		c.Playlists = append(c.Playlists, cloud.List{ID: p.ID, Name: p.Name, Tracks: p.TrackIDs})
	}
	for _, id := range c.TrackIDs() {
		if b := songs[id]; len(b) > 0 {
			if c.Songs == nil {
				c.Songs = map[string]json.RawMessage{}
			}
			c.Songs[id] = b
		}
	}
	return c, songs, nil
}

func (g GDCollection) Snapshot() (cloud.Collection, error) {
	p := g.Player
	p.mu.Lock()
	defer p.mu.Unlock()
	c, _, err := gdSnapshot(p.storage)
	return c, err
}

func (g GDCollection) ApplyCollection(before, accepted cloud.Collection) error {
	p := g.Player
	p.mu.Lock()
	defer p.mu.Unlock()
	current, songs, err := gdSnapshot(p.storage)
	if err != nil {
		return err
	}
	result := cloud.MergeCollection(before, current, accepted)
	if reflect.DeepEqual(current, result) {
		return nil
	}
	if songs == nil {
		songs = map[string]json.RawMessage{}
	}
	maps.Copy(songs, result.Songs)
	lists := make([]gdList, 0, len(result.Playlists))
	for _, l := range result.Playlists {
		lists = append(lists, gdList{l.ID, l.Name, true, l.Tracks})
	}
	next := maps.Clone(p.storage)
	for k, v := range map[string]any{"liked": result.Likes, "playlists": lists, "songs": songs} {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		next[k] = b
	}
	if err = p.writeJSON("storage.json", next); err != nil {
		return err
	}
	p.storage = next
	return nil
}
