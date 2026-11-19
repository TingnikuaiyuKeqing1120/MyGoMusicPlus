package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"mygo-music/internal/i18n"
	cloud "mygo-music/internal/sync"
)

type syncTransport struct{ handler http.Handler }

func (tr syncTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := httptest.NewRecorder()
	tr.handler.ServeHTTP(r, req)
	return r.Result(), nil
}

// The normal Sync Now entry point transfers a previously unknown plugin and
// its live JSON data to an empty phone, then merges edits back to the desktop.
func TestSyncNowGenericPluginRoundTrip(t *testing.T) {
	language := i18n.Language()
	t.Cleanup(func() { i18n.SetLanguage(language) })
	var mu sync.Mutex
	files := map[string][]byte{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/user" {
			fmt.Fprint(w, `{"login":"fixture"}`)
			return
		}
		if r.URL.Path == "/repos/fixture/sync" {
			fmt.Fprint(w, `{}`)
			return
		}
		const prefix = "/repos/fixture/sync/contents/"
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.Error(w, `{}`, 404)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, prefix)
		data, exists := files[path]
		rev := ""
		if exists {
			rev = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		switch r.Method {
		case "GET":
			if !exists {
				http.Error(w, `{}`, 404)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"sha": rev, "encoding": "base64", "content": base64.StdEncoding.EncodeToString(data)})
		case "PUT":
			var in struct{ Sha, Content string }
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				http.Error(w, `{}`, 400)
				return
			}
			if in.Sha != rev {
				http.Error(w, `{}`, 409)
				return
			}
			incoming, err := base64.StdEncoding.DecodeString(in.Content)
			if err != nil {
				http.Error(w, `{}`, 400)
				return
			}
			files[path] = incoming
			w.WriteHeader(201)
			fmt.Fprint(w, `{}`)
		default:
			http.Error(w, `{}`, 405)
		}
	})
	previous := newSyncRepository
	newSyncRepository = func(token, repo string) *cloud.GitHub {
		return &cloud.GitHub{Token: token, Repo: repo, Client: &http.Client{Transport: syncTransport{handler}, Timeout: time.Second}}
	}
	root, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { newSyncRepository = previous; mygo.App.SetPath(mygo.PathUserData, root) })
	leftRoot, rightRoot := t.TempDir(), t.TempDir()
	pluginDir := filepath.Join(leftRoot, "plugins", "new-radio")
	if err = os.MkdirAll(pluginDir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, code := range map[string]string{
		"plugin.json": `{"id":"new-radio","name":"New Radio"}`,
		"index.js":    `export function playlists(){return host.storage.get("lists")||[]} export function createPlaylist({name}){const a=host.storage.get("lists")||[];const p={id:name,name,mine:true};a.push(p);host.storage.set("lists",a);return p}`,
	} {
		if err = os.WriteFile(filepath.Join(pluginDir, name), []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
	}
	makeApp := func(path string) *app {
		mygo.App.SetPath(mygo.PathUserData, path)
		a := newTestApp()
		a.settings = loadSettings()
		a.makeSources()
		a.syncer.cfg = cloud.Config{Token: "fixture", Repo: "sync", Units: map[cloud.Unit]bool{cloud.UnitSettings: true, cloud.UnitPlaylists: true, cloud.UnitPlugins: true}}
		t.Cleanup(func() { a.syncer.cfg.Token = ""; a.postFn = nil; a.closeSources() })
		return a
	}
	left, right := makeApp(leftRoot), makeApp(rightRoot)
	if _, err = left.plugins[0].CreatePlaylist("desktop"); err != nil {
		t.Fatal(err)
	}
	left.settings.Quality = "lossless"
	left.settings.Source = "new-radio"
	run := func(path string, a *app) {
		t.Helper()
		mygo.App.SetPath(mygo.PathUserData, path)
		ch := posts(a)
		a.syncNow()
		for a.syncer.busy {
			select {
			case fn := <-ch:
				onMainThread(fn)
			case <-time.After(10 * time.Second):
				t.Fatal("sync did not complete")
			}
		}
		if a.syncer.err != "" {
			t.Fatal(a.syncer.err)
		}
		a.postFn = nil
	}
	run(rightRoot, right)
	run(leftRoot, left)
	run(rightRoot, right)
	if len(right.plugins) != 1 {
		t.Fatal("new plugin was not installed", len(right.plugins))
	}
	lists, err := right.plugins[0].Playlists()
	if err != nil || len(lists) != 1 || lists[0].ID != "desktop" {
		t.Fatal(lists, err)
	}
	if right.settings.Quality != "lossless" {
		t.Fatal("first phone preferences erased the desktop's setting")
	}
	if right.library.id != "new-radio" {
		t.Fatal("active source was not applied", right.library.id)
	}
	if _, err = right.plugins[0].CreatePlaylist("phone"); err != nil {
		t.Fatal(err)
	}
	run(rightRoot, right)
	run(leftRoot, left)
	lists, err = left.plugins[0].Playlists()
	if err != nil || len(lists) != 2 {
		t.Fatal(lists, err)
	}
	right.settings.Source = "local"
	run(rightRoot, right)
	run(leftRoot, left)
	if left.library.id != "local" {
		t.Fatal("reset to local source was not applied", left.library.id)
	}
}
