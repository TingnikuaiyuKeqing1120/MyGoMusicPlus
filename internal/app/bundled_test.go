package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mygo-music/internal/plugin"
)

// The built-in GD plugin installs in one click with the repository out of
// reach: the copy embedded in the app stands in, and is of the repository
// still, so it can be checked for updates. Its staging used to sit in /tmp,
// which a browser's filesystem doesn't have, and the install failed on a
// phone with "stat /tmp: no such file or directory".
func TestInstallBundledGD(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	was := fetchPlugin
	defer func() { fetchPlugin = was }()
	var asked string
	fetchPlugin = func(repo, dir string) (*plugin.Pending, error) {
		asked = repo
		return nil, errors.New("the repository can't be reached")
	}
	if a.hasBundledGD() {
		t.Fatal("the built-in plugin is installed before it was asked for")
	}
	a.installBundledGD()
	if !a.installer.open || !a.installer.busy {
		t.Fatal("the install dialog didn't open on its way")
	}
	until(t, ch, func() bool { return a.installer.pending != nil })
	if asked != bundledGDRepo {
		t.Fatalf("asked for %q, not the built-in plugin's repository", asked)
	}
	if p := a.installer.pending; p.Manifest.ID != bundledGDID {
		t.Fatalf("asked about %s, not the built-in plugin", p.Manifest.ID)
	}
	a.confirmInstall()
	until(t, ch, func() bool { return !a.installer.busy })
	if a.installer.err != "" {
		t.Fatalf("installing: %s", a.installer.err)
	}
	if !a.hasBundledGD() {
		t.Fatal("the built-in plugin isn't a source after installing")
	}
	if _, err := os.Stat(filepath.Join(pluginsDir(), bundledGDID, "index.js")); err != nil {
		t.Fatalf("the plugin's files: %v", err)
	}
	var lib *library
	for _, l := range a.allLibs {
		if l.id == bundledGDID {
			lib = l
		}
	}
	if o := pluginOf(lib).Origin(); o == nil || o.Repo != bundledGDRepo {
		t.Fatalf("the embedded copy's origin %+v, not the repository's", o)
	}
}

// With the repository reachable, its latest release is what's asked about,
// not the copy embedded in the app.
func TestInstallBundledGDFromRepo(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	was := fetchPlugin
	defer func() { fetchPlugin = was }()
	fetchPlugin = func(repo, dir string) (*plugin.Pending, error) {
		if repo != bundledGDRepo {
			t.Errorf("asked for %q, not the built-in plugin's repository", repo)
		}
		p, err := stageBundledGD()
		if err != nil {
			return nil, err
		}
		p.Version = "9.9.9" // the repository's, newer than the embedded copy
		return p, nil
	}
	a.installBundledGD()
	until(t, ch, func() bool { return a.installer.pending != nil })
	a.confirmInstall()
	until(t, ch, func() bool { return !a.installer.busy })
	if a.installer.err != "" {
		t.Fatalf("installing: %s", a.installer.err)
	}
	for _, l := range a.allLibs {
		if l.id == bundledGDID {
			if o := pluginOf(l).Origin(); o == nil || o.Version != "9.9.9" {
				t.Fatalf("installed %+v, not the repository's release", o)
			}
			return
		}
	}
	t.Fatal("the built-in plugin isn't a source after installing")
}