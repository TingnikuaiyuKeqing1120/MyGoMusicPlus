//go:build js

package app

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// Browsers have no native file dialogs: songs save to a Downloads folder in
// the app's own data directory (kept by the web shell's persistent filesystem),
// without asking where. Exporting to a real file is the shell's job later.
func webDownloadDir() string {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		dir = "/mygo-data"
	}
	d := filepath.Join(dir, "Downloads")
	os.MkdirAll(d, 0o700)
	return d
}

func init() {
	downloadDir = webDownloadDir
	saveSongAs = func(a *app, name string) string {
		return filepath.Join(webDownloadDir(), name)
	}
	pickFolder = func(a *app, got func(string)) {
		got(webDownloadDir())
	}
}
