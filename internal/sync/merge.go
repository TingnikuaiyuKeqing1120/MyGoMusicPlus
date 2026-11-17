package sync

import "encoding/json"

// libraryJSON is local/library.json as the merge reads it: the built-in
// source's folders, likes, playlists and recent plays.
type libraryJSON struct {
	Folders   []string       `json:"folders,omitempty"`
	Likes     []string       `json:"likes,omitempty"`
	Playlists []playlistJSON `json:"playlists,omitempty"`
	Recent    []string       `json:"recent,omitempty"`
}

type playlistJSON struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Tracks []string `json:"tracks,omitempty"`
}

// mergeLibrary combines two library.json files. This device keeps its own
// folders and recent plays, which are its alone; likes union, and playlists
// union by ID, one both have taken from whichever side wrote it last.
func mergeLibrary(local, remote []byte, remoteNewer bool) []byte {
	var l, r libraryJSON
	if json.Unmarshal(local, &l) != nil {
		return remote
	}
	if json.Unmarshal(remote, &r) != nil {
		return local
	}
	out := l
	seen := map[string]bool{}
	var likes []string
	for _, id := range r.Likes {
		if !seen[id] {
			seen[id] = true
			likes = append(likes, id)
		}
	}
	for _, id := range l.Likes {
		if !seen[id] {
			seen[id] = true
			likes = append(likes, id)
		}
	}
	out.Likes = likes
	byID := map[string]playlistJSON{}
	var order []string
	for _, p := range l.Playlists {
		byID[p.ID] = p
		order = append(order, p.ID)
	}
	for _, p := range r.Playlists {
		if _, ok := byID[p.ID]; !ok {
			order = append(order, p.ID)
			byID[p.ID] = p
			continue
		}
		if remoteNewer {
			byID[p.ID] = p
		}
	}
	out.Playlists = nil
	for _, id := range order {
		out.Playlists = append(out.Playlists, byID[id])
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return local
	}
	return b
}