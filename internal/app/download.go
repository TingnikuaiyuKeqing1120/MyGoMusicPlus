package app

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-music/internal/local"
	"mygo-music/internal/music"
)

// saveSongAs asks the listener where to save a song with the native dialog,
// suggesting name in their Downloads folder, and returns the path, or "" if
// they didn't pick one. It blocks, so it runs on a goroutine; tests replace it.
var saveSongAs = func(a *app, name string) string {
	dir, _ := mygo.App.Path(mygo.PathDownloads)
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:            a.win,
		Title:             T("Download"),
		DefaultPath:       filepath.Join(dir, name),
		ButtonLabel:       T("Download"),
		CreateDirectories: true,
	})
	if err != nil {
		return ""
	}
	return path
}

var downloadDir = func() string {
	dir, _ := mygo.App.Path(mygo.PathDownloads)
	return dir
}

// canDownload reports whether tr can be saved to a file of the listener's: a
// song its source streams for the app to play, rather than one already on disk
// or one that plays elsewhere.
func (a *app) canDownload(tr *music.Track) bool {
	lib := a.trackLib(tr)
	return lib != nil && lib.id != local.ID && lib.id != downloadedID && lib.can("stream") &&
		lib.src.Info().Playback == music.PlaybackStream && !tr.Unavailable
}

// canDownloadAny reports whether any of tracks can be saved to a file.
func (a *app) canDownloadAny(tracks []music.Track) bool {
	for i := range tracks {
		if a.canDownload(&tracks[i]) {
			return true
		}
	}
	return false
}

// pickFolder asks the listener for one folder to save songs into, and calls
// got with it on the main thread if they pick one. It blocks, so it runs on a
// goroutine; tests replace it.
var pickFolder = func(a *app, got func(string)) {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:             T("Download All"),
			ButtonLabel:       T("Download"),
			Directory:         true,
			CreateDirectories: true,
		})
		if err == nil && len(paths) > 0 {
			a.post(func() { got(paths[0]) })
		}
	}()
}

// downloadQualityMenu asks which quality to save tracks at, then downloads
// them all into one folder the listener picks.
func (a *app) downloadQualityMenu(m *ui.Menu, tracks []music.Track) {
	for _, q := range qualityLevels {
		if m.Item(T(q.label)).Chosen() {
			a.downloadAll(tracks, q.level)
		}
	}
}

// downloadAll saves copies of tracks into one folder the listener picks, at
// the quality asked, from the cache if kept there. A song listed twice is
// saved once, and one already in the folder is left as it is. The download
// manager shows each one's progress, and a toast tells of them all.
func (a *app) downloadAll(tracks []music.Track, quality string) {
	var list []music.Track
	seen := map[string]bool{}
	for _, tr := range tracks {
		if tr.Source == "" {
			tr.Source = a.library.id
		}
		key := tr.Source + "\x00" + tr.ID
		if !seen[key] && a.canDownload(&tr) {
			seen[key] = true
			list = append(list, tr)
		}
	}
	if len(list) == 0 {
		return
	}
	pickFolder(a, func(dir string) {
		a.downloads.addBatch(a, quality, list, dir)
		a.downloads.open = true
	})
}

// download saves a copy of tr where the listener picks, at the quality the
// settings say, from the cache if it's kept there; the download manager shows
// its progress, and a toast tells when it lands.
func (a *app) download(tr music.Track) {
	if !a.canDownload(&tr) {
		return
	}
	lib := a.trackLib(&tr)
	a.downloads.add(a, &dlJob{track: tr, lib: lib, quality: a.settings.Quality, choosePath: true})
	a.downloads.open = true
}

// songFileName names the file of a song of type ext as its credits read,
// "Artists - Title.ext", with what no file name may hold taken out.
func songFileName(tr *music.Track, ext string) string {
	name := tr.Name
	if artists := tr.ArtistNames(); artists != "" {
		name = artists + " - " + name
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, name)
	// Most systems take 255 bytes, with room left for the extension.
	for len(name) > 200 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	// Windows drops a trailing dot or space, and a leading dot hides the file.
	name = strings.Trim(name, ". ")
	if name == "" {
		name = T("Song")
	}
	return name + "." + ext
}
