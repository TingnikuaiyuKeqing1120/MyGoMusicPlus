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

// bundledGDRepo is where the built-in plugin is maintained: the one-click
// install pulls its latest release from there, so it stays updatable, and
// falls back to the copy embedded in the app when there's no network.
const bundledGDRepo = "TingnikuaiyuKeqing1120/gd-music-plugin"

// hasBundledGD reports whether the built-in plugin is installed already.
func (a *app) hasBundledGD() bool {
	return slices.ContainsFunc(a.allLibs, func(l *library) bool { return l.id == bundledGDID })
}

// installBundledGD offers the built-in plugin to install, asking about it as
// one from GitHub is asked about: its latest release from the repository, or
// the copy embedded in the app if the repository can't be reached.
func (a *app) installBundledGD() {
	in := &a.installer
	a.cancelInstall()
	in.open, in.github, in.busy, in.err = true, false, true, ""
	go func() {
		pending, err := fetchPlugin(bundledGDRepo, pluginsDir())
		if err != nil {
			pending, err = stageBundledGD()
		}
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

// stageBundledGD writes the embedded copy out to ask about it, as one picked
// from a folder, but of the repository so it can be checked for updates. The
// staging folder sits with the plugins, not in /tmp, which a browser's
// filesystem doesn't have.
func stageBundledGD() (*plugin.Pending, error) {
	base := pluginsDir()
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(base, ".gd-stage-")
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
	p, err := openPlugin(tmp, pluginsDir())
	if err != nil {
		return nil, err
	}
	p.Repo = bundledGDRepo
	return p, nil
}