package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"

	"mygo-music/internal/music"
)

// downloadedID is the source of the songs saved to files of the listener's,
// which the Downloaded page lists and plays from disk. A track of it has the
// file's path for an ID, so the source answers the file itself, stateless.
const downloadedID = "downloaded"

// dlSource plays the songs the download manager saved, from their files. It
// keeps nothing of its own: a track's ID is the path of its file.
type dlSource struct{}

func (dlSource) Info() music.Info {
	return music.Info{ID: downloadedID, Name: T("Downloaded"), Playback: music.PlaybackStream}
}

func (dlSource) Can(what string) bool { return what == "stream" || what == "lyrics" }

func (dlSource) Stream(id, quality string) (*music.Stream, error) {
	f, err := os.Open(id)
	if err != nil {
		return nil, music.ErrUnavailable
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, music.ErrUnavailable
	}
	typ := strings.ToLower(strings.TrimPrefix(filepath.Ext(id), "."))
	switch typ {
	case "mp3", "flac", "ogg", "wav", "m4a", "aac":
	default:
		typ = "mp3"
	}
	quality = "high"
	if typ == "flac" || typ == "wav" {
		quality = "lossless"
	}
	return &music.Stream{URL: music.FileURL(id), Type: typ, Quality: quality, Size: info.Size()}, nil
}

// Lyrics answers the .lrc beside the file, if there is one.
func (dlSource) Lyrics(id string) (*music.Lyrics, error) {
	base := strings.TrimSuffix(id, filepath.Ext(id))
	for _, ext := range []string{".lrc", ".LRC"} {
		if b, err := os.ReadFile(base + ext); err == nil {
			return &music.Lyrics{LRC: strings.TrimPrefix(string(b), "\xef\xbb\xbf")}, nil
		}
	}
	return &music.Lyrics{}, nil
}

func (dlSource) Setting(string) string                { return "" }
func (dlSource) SetSetting(string, string) error      { return music.ErrUnsupported }
func (dlSource) SignIn() (*music.Account, error)      { return nil, music.ErrUnsupported }
func (dlSource) SignOut() error                       { return music.ErrUnsupported }
func (dlSource) Account() (*music.Account, error)     { return nil, nil }
func (dlSource) Suggest(string) ([]string, error)     { return nil, music.ErrUnsupported }
func (dlSource) Playlists() ([]music.Playlist, error) { return nil, music.ErrUnsupported }
func (dlSource) Playlist(string) (*music.Playlist, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) Album(string) (*music.AlbumPage, error) { return nil, music.ErrUnsupported }
func (dlSource) Artist(string) (*music.ArtistPage, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) Liked(int) (*music.Page, error) { return nil, music.ErrUnsupported }
func (dlSource) Recent() ([]music.Track, error) { return nil, music.ErrUnsupported }
func (dlSource) Home() (*music.Browse, error)   { return nil, music.ErrUnsupported }
func (dlSource) Browse(string) (*music.Browse, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) Search(string, music.SearchKind, int) (*music.SearchResult, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) IsLiked([]string) (map[string]bool, error) { return nil, music.ErrUnsupported }
func (dlSource) Like(string, bool) error                   { return music.ErrUnsupported }
func (dlSource) LikeEpisode(string, bool) error            { return music.ErrUnsupported }
func (dlSource) CreatePlaylist(string) (*music.Playlist, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) RenamePlaylist(string, string) error       { return music.ErrUnsupported }
func (dlSource) DeletePlaylist(string) error               { return music.ErrUnsupported }
func (dlSource) AddToPlaylist(string, []string) error      { return music.ErrUnsupported }
func (dlSource) RemoveFromPlaylist(string, []string) error { return music.ErrUnsupported }
func (dlSource) MovePlaylistTrack(string, int, int) error  { return music.ErrUnsupported }
func (dlSource) SavePlaylist(string, bool) error           { return music.ErrUnsupported }
func (dlSource) FollowArtist(string, bool) error           { return music.ErrUnsupported }
func (dlSource) SaveAlbum(string, bool) error              { return music.ErrUnsupported }
func (dlSource) SavedAlbums(int) (*music.AlbumList, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) Shows() ([]music.Show, error) { return nil, music.ErrUnsupported }
func (dlSource) Show(string, int) (*music.ShowPage, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) SubscribeShow(string, bool) error { return music.ErrUnsupported }
func (dlSource) Stations() ([]music.Station, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) StationNext(string) ([]music.Track, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) Comments(string, int) (*music.CommentPage, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) LikeComment(string, string, bool) error { return music.ErrUnsupported }
func (dlSource) PostComment(string, string) (*music.Comment, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) RemotePlay([]string, int, int64) error { return music.ErrUnsupported }
func (dlSource) RemotePause() error                    { return music.ErrUnsupported }
func (dlSource) RemoteResume() error                   { return music.ErrUnsupported }
func (dlSource) RemoteSeek(int64) error                { return music.ErrUnsupported }
func (dlSource) RemoteVolume(int) error                { return music.ErrUnsupported }
func (dlSource) RemoteState() (*music.RemoteState, error) {
	return nil, music.ErrUnsupported
}
func (dlSource) RemoteDevices() ([]music.Device, error) { return nil, music.ErrUnsupported }
func (dlSource) RemoteUse(string) error                 { return music.ErrUnsupported }

var _ music.Source = dlSource{}

// savedJobs are the songs saved to files, newest first, for the Downloaded page.
func (m *dlManager) savedJobs() []*dlJob {
	var out []*dlJob
	seen := map[string]bool{}
	for _, j := range slices.Backward(m.jobs) {
		if j.status == dlDone && !seen[j.path] {
			out = append(out, j)
			seen[j.path] = true
		}
	}
	return out
}

// downloadedTrack is a saved song as the Downloaded page plays it: from its
// file, under the downloaded source, with the file's path for an ID.
func downloadedTrack(j *dlJob) music.Track {
	tr := j.track
	tr.Source, tr.ID, tr.Unavailable = downloadedID, j.path, false
	return tr
}

// drop forgets the jobs ids, keeping the rest on disk as they are.
func (m *dlManager) drop(ids []int) {
	set := make(map[int]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	m.jobs = slices.DeleteFunc(m.jobs, func(j *dlJob) bool { return set[j.id] })
	m.persist()
}

// revealFile shows a saved song in the file manager; trashFile moves it to the
// Trash. Tests replace them.
var (
	revealFile = mygo.Shell.ShowItemInFolder
	trashFile  = mygo.Shell.TrashItem
)

// confirmDeleteDownload asks the listener with a native dialog whether to
// delete files, on a goroutine for it blocks, calling yes if they say so;
// tests replace it.
var confirmDeleteDownload = func(a *app, message string, yes func()) {
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:             a.win,
			Type:               mygo.MessageWarning,
			Message:            message,
			Detail:             T("The files move to the Trash."),
			Buttons:            []string{T("Cancel"), T("Delete")},
			DefaultButton:      1,
			DestructiveButtons: []int{1},
		})
		if err == nil && res.Button == 1 {
			a.post(yes)
		}
	}()
}

// askDeleteDownloads asks whether to delete the saved songs sel, then moves
// their files to the Trash and forgets them.
func (a *app) askDeleteDownloads(sel []music.Track) {
	if len(sel) == 0 {
		return
	}
	jobs := a.downloads.jobsForTracks(sel)
	if len(jobs) == 0 {
		return
	}
	message := Tf("Delete %s?", jobs[0].track.Name)
	if len(jobs) > 1 {
		message = Tf("Delete %d songs?", len(jobs))
	}
	confirmDeleteDownload(a, message, func() { a.deleteDownloads(jobs) })
}

// deleteDownloads moves the jobs' files to the Trash and forgets them, off the
// main thread for the Trash may take a moment.
func (a *app) deleteDownloads(jobs []*dlJob) {
	paths := make([]string, len(jobs))
	ids := make([]int, len(jobs))
	for i, j := range jobs {
		paths[i], ids[i] = j.path, j.id
	}
	n := len(jobs)
	go func() {
		for _, p := range paths {
			trashFile(p)
		}
		a.post(func() {
			a.downloads.drop(ids)
			a.trackSel.Clear()
			a.toast = Tf("Deleted %d songs", n)
		})
	}()
}

// jobsForTracks returns the saved jobs of tracks, which carry their file's path
// for an ID, each once.
func (m *dlManager) jobsForTracks(tracks []music.Track) []*dlJob {
	var out []*dlJob
	seen := map[int]bool{}
	for _, tr := range tracks {
		for _, j := range m.jobs {
			if j.status == dlDone && j.path == tr.ID && !seen[j.id] {
				seen[j.id] = true
				out = append(out, j)
			}
		}
	}
	return out
}
