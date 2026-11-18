package sync

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
)

// Collection contains only user-selected tracks and the metadata needed to
// display them. Device folders, search caches and account data stay local.
type Collection struct {
	Likes     []string                   `json:"likes,omitempty"`
	Playlists []List                     `json:"playlists,omitempty"`
	Songs     map[string]json.RawMessage `json:"songs,omitempty"`
}

type List struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Tracks []string `json:"tracks,omitempty"`
}

func (c Collection) empty() bool { return len(c.Playlists) == 0 && len(c.Likes) == 0 }

func (c Collection) TrackIDs() []string {
	ids := slices.Clone(c.Likes)
	for _, p := range c.Playlists {
		ids = append(ids, p.Tracks...)
	}
	return ids
}

// CollectionSource applies an accepted change through the live source,
// merging edits made while GitHub was answering with the accepted result.
type CollectionSource interface {
	Snapshot() (Collection, error)
	ApplyCollection(before, accepted Collection) error
}

type CollectionStore interface {
	ReadVersion(path string) (data []byte, revision string, err error)
	PutVersion(path string, data []byte, revision string) error
}

var ErrConflict = errors.New("sync data changed on another device")

func listMap(lists []List) (map[string]List, []string) {
	m := make(map[string]List, len(lists))
	var ids []string
	for _, p := range lists {
		m[p.ID] = p
		ids = append(ids, p.ID)
	}
	return m, ids
}

// The common ancestor distinguishes a removal from a new device which has
// never seen the entry. An edit to a shared list retains concurrent additions.
func mergeIDs(base, local, remote []string) []string {
	set := func(ids []string) map[string]bool {
		m := make(map[string]bool, len(ids))
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	b, l, r := set(base), set(local), set(remote)
	first, second := local, remote
	if slices.Equal(local, base) {
		first, second = remote, local
	}
	seen := map[string]bool{}
	var out []string
	for _, ids := range [][]string{first, second} {
		for _, id := range ids {
			if !seen[id] && (!b[id] || l[id] && r[id]) {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func MergeCollection(base, local, remote Collection) Collection {
	out := Collection{Likes: mergeIDs(base.Likes, local.Likes, remote.Likes)}
	b, bi := listMap(base.Playlists)
	l, li := listMap(local.Playlists)
	r, ri := listMap(remote.Playlists)
	ids := mergeIDs(bi, li, ri)
	// If one side removed the list while the other edited it, keep the edit.
	for _, id := range bi {
		lp, lok := l[id]
		rp, rok := r[id]
		if lok == rok {
			continue
		}
		p := lp
		if rok {
			p = rp
		}
		if p.Name != b[id].Name || !slices.Equal(p.Tracks, b[id].Tracks) {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		lp, lok := l[id]
		rp, rok := r[id]
		p := lp
		if !lok {
			p = rp
		} else if rok {
			if lp.Name == b[id].Name {
				p.Name = rp.Name
			}
			p.Tracks = mergeIDs(b[id].Tracks, lp.Tracks, rp.Tracks)
		}
		out.Playlists = append(out.Playlists, p)
	}
	for _, id := range out.TrackIDs() {
		v := local.Songs[id]
		if len(v) == 0 || bytes.Equal(v, base.Songs[id]) && len(remote.Songs[id]) != 0 {
			v = remote.Songs[id]
		}
		if len(v) > 0 {
			if out.Songs == nil {
				out.Songs = map[string]json.RawMessage{}
			}
			out.Songs[id] = v
		}
	}
	return out
}

type CollectionResult struct {
	Changed          bool
	Playlists, Likes int
}

// SyncCollection commits the merged snapshot against the revision actually
// read. A retry re-reads both sides. Only a successful apply advances the
// common ancestor for this account and repository.
func SyncCollection(store CollectionStore, root, repoID, sourceID string, source CollectionSource) (CollectionResult, error) {
	var result CollectionResult
	path := "collections/" + sourceID + ".json"
	checkpoint := filepath.Join(root, "sync-history", fmt.Sprintf("%x", sha256.Sum256([]byte(repoID))), sourceID+".json")
	var base Collection
	saved, err := os.ReadFile(checkpoint)
	if err == nil {
		if err = json.Unmarshal(saved, &base); err != nil {
			return result, err
		}
	} else if !os.IsNotExist(err) {
		return result, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		raw, rev, err := store.ReadVersion(path)
		if err != nil {
			return result, err
		}
		var remote Collection
		if raw != nil {
			if err = json.Unmarshal(raw, &remote); err != nil {
				return result, err
			}
		} else {
			base = Collection{}
			// Seed the built-in collection from the original sync format.
			if sourceID == "local" {
				legacy, _, err := store.ReadVersion("data/local/library.json")
				if err != nil {
					return result, err
				}
				if legacy != nil {
					if err = json.Unmarshal(legacy, &remote); err != nil {
						return result, err
					}
				}
			}
		}
		local, err := source.Snapshot()
		if err != nil {
			return result, err
		}
		ancestor := base
		// An entirely empty library can also mean a cleared or restored
		// profile. Do not infer a request to erase the other device from it.
		if local.empty() || remote.empty() {
			ancestor = Collection{}
		}
		merged := MergeCollection(ancestor, local, remote)
		data, err := json.Marshal(merged)
		if err != nil {
			return result, err
		}
		if !bytes.Equal(raw, data) && (raw != nil || len(merged.Likes)+len(merged.Playlists) > 0) {
			err = store.PutVersion(path, data, rev)
			if errors.Is(err, ErrConflict) {
				continue
			}
			if err != nil {
				return result, err
			}
		}
		result = CollectionResult{Changed: !reflect.DeepEqual(local, merged), Playlists: len(merged.Playlists), Likes: len(merged.Likes)}
		if result.Changed {
			if err = source.ApplyCollection(local, merged); err != nil {
				return result, err
			}
		}
		if bytes.Equal(saved, data) {
			return result, nil
		}
		return result, writeAtomic(checkpoint, data)
	}
	return result, ErrConflict
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".sync-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
