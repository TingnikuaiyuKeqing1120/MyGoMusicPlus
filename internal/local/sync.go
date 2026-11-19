package local

import (
	"encoding/json"
	"reflect"
	"slices"

	cloud "mygo-music/internal/sync"
)

func (s *Source) collection() cloud.Collection {
	out := cloud.Collection{Likes: slices.Clone(s.lib.Likes), Recent: slices.Clone(s.lib.Recent)}
	for _, p := range s.lib.Playlists {
		out.Playlists = append(out.Playlists, cloud.List{ID: p.ID, Name: p.Name, Tracks: slices.Clone(p.Tracks)})
	}
	return out
}

func (s *Source) Snapshot() (cloud.Collection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.collection(), nil
}

func (s *Source) ApplyCollection(before, accepted cloud.Collection) error {
	// Serialize with change's pending writes. Raising saved below prevents
	// an older snapshot waiting on saveMu from replacing the merged library.
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.collection()
	merged := cloud.MergeCollection(before, current, accepted)
	if reflect.DeepEqual(current, merged) {
		return nil
	}
	next := s.lib
	next.Likes = merged.Likes
	next.Recent = merged.Recent[:min(len(merged.Recent), keepRecent)]
	next.Playlists = nil
	for _, p := range merged.Playlists {
		next.Playlists = append(next.Playlists, playlist(p))
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = writeAtomic(s.libraryPath(), data); err != nil {
		return err
	}
	s.lib = next
	s.saves++
	s.saved = s.saves
	return nil
}
