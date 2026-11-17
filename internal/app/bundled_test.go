package app

import (
	"os"
	"path/filepath"
	"testing"
)

// The built-in GD plugin installs in one click: asked about as one from this
// computer is, then in, a source of the app's.
func TestInstallBundledGD(t *testing.T) {
	a := newTestApp()
	ch := posts(a)
	if a.hasBundledGD() {
		t.Fatal("the built-in plugin is installed before it was asked for")
	}
	a.installBundledGD()
	if !a.installer.open || !a.installer.busy {
		t.Fatal("the install dialog didn't open on its way")
	}
	until(t, ch, func() bool { return a.installer.pending != nil })
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
}