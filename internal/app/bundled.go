package app

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"mygo-music/internal/plugin"
)

// The GD music source comes with the app, to install in one click from the
// plugins' settings, as one from a folder is asked about before it's in.

//go:embed bundled/gdstudio
var bundledGD embed.FS

// bundledGDID is the built-in plugin's ID, as its manifest says.
const bundledGDID = "gdstudio"

// hasBundledGD reports whether the built-in plugin is installed already.
func (a *app) hasBundledGD() bool {
	return slices.ContainsFunc(a.allLibs, func(l *library) bool { return l.id == bundledGDID })
}

// installBundledGD offers the built-in plugin to install, asking about it as
// one from this computer is asked about.
func (a *app) installBundledGD() {
	in := &a.installer
	a.cancelInstall()
	in.open, in.github, in.busy, in.err = true, false, true, ""
	go func() {
		pending, err := stageBundledGD()
		a.post(func() {
			in.busy = false
			if err != nil {
				in.err = installError(err)
				return
			}
			in.pending = pending
		})
	}()
}

// stageBundledGD writes the built-in plugin out to ask about it, as one
// picked from a folder.
func stageBundledGD() (*plugin.Pending, error) {
	tmp, err := os.MkdirTemp("", "mygo-gd-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	err = fs.WalkDir(bundledGD, "bundled/gdstudio", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		b, err := bundledGD.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o644)
	})
	if err != nil {
		return nil, err
	}
	return openPlugin(tmp, pluginsDir())
}