package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"mygo-music/internal/music"
)

// The bundled GD module must run in the actual plugin VM, reuse album artwork and preserve liked tracks.
func TestGDStudioRequestCache(t *testing.T) {
	var searches, pictures atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("types") {
		case "search":
			searches.Add(1)
			json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "1", "name": "First", "artist": []string{"Artist"}, "album": "Album", "pic_id": "shared", "extra_data": map[string]any{"duration": 180}},
				map[string]any{"id": "2", "name": "Second", "artist": []string{"Artist"}, "album": "Album", "pic_id": "shared"},
			})
		case "pic":
			pictures.Add(1)
			w.Write([]byte(`{"url":"https://example.test/cover.jpg"}`))
		case "url":
			w.Write([]byte(`{"url":"https://example.test/song.flac","br":740,"size":40000}`))
		default:
			t.Errorf("unexpected API call %s", r.URL)
		}
	}))
	defer server.Close()
	source, err := os.ReadFile(filepath.Join("..", "app", "bundled", "gdstudio", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join("..", "app", "bundled", "gdstudio", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	p := load(t, strings.ReplaceAll(string(manifest), "music-api.gdstudio.xyz", "127.0.0.1"), strings.ReplaceAll(string(source), "https://music-api.gdstudio.xyz/api.php", server.URL))
	var id string
	for range 2 {
		page, err := p.Search("album", music.SearchTracks, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Tracks) != 2 || page.Tracks[0].Duration != 180000 || page.Tracks[1].Duration != 0 {
			t.Fatalf("metadata: %+v", page.Tracks)
		}
		for _, tr := range page.Tracks {
			if tr.Album.ImageURL != "https://example.test/cover.jpg" || tr.Source != "gdstudio" {
				t.Fatalf("cover or identity: %+v", tr)
			}
		}
		id = page.Tracks[0].ID
	}
	if searches.Load() != 1 || pictures.Load() != 1 {
		t.Fatalf("duplicate requests: searches=%d pictures=%d", searches.Load(), pictures.Load())
	}
	if err := p.Like(id, true); err != nil {
		t.Fatal(err)
	}
	page, err := p.Liked(0)
	if err != nil || len(page.Tracks) != 1 || page.Tracks[0].ID != id {
		t.Fatalf("liked track lost: %+v %v", page, err)
	}
	stream, err := p.Stream(id, "lossless")
	if err != nil || stream.Size != 0 || stream.Quality != "lossless" {
		t.Fatalf("stream: %+v %v", stream, err)
	}
	document, err := p.SnapshotData()
	if err != nil {
		t.Fatal(err)
	}
	var storage map[string]json.RawMessage
	if err := json.Unmarshal(document["storage"], &storage); err != nil {
		t.Fatal(err)
	}
	if storage["liked"] == nil || storage["songs"] == nil || storage["requestCache"] != nil || storage["coverCache"] != nil {
		t.Fatalf("sync included device cache or lost user data: %v", storage)
	}
}
