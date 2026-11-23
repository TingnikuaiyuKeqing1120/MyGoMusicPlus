package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"mygo-music/internal/music"
	"mygo-music/internal/player"
)

// The download manager saves songs to files of the listener's, a few at a
// time, showing each one's progress and taking cancel and retry. What it has
// saved is kept on disk, for the downloads to show again after a restart.
// Its jobs are the main thread's: what a download's goroutine has to say it
// posts, as the rest of the app does.

type dlStatus int

const (
	dlQueued dlStatus = iota
	dlDownloading
	dlDone
	dlFailed
	dlCanceled
	dlPaused
)

// dlLimit is how many songs download at once.
const dlLimit = 3

// dlJob is one song on its way to a file: where it goes, how far it has come,
// and what became of it.
type dlJob struct {
	id           int
	track        music.Track
	dir          string // the folder to save into, when the path isn't set yet
	path         string // the file it goes to, once known
	status       dlStatus
	done         int64
	size         int64
	err          string
	cancel       context.CancelFunc
	pausing      bool // a pause was asked, to keep the part rather than drop it
	lib          *library
	quality      string
	group        *dlGroup // the batch it is one of, or nil for a single download
	at           time.Time
	choosePath   bool
	hidden       bool
	skipped      bool
	variant      bool // disambiguate equal filenames belonging to different tracks
	sizeVerified bool // size observed on disk after completion, rather than a source hint
}

// dlGroup is a batch downloaded together, whose end one toast tells of.
type dlGroup struct {
	total, finished, saved, failed, skipped int
}

type dlManager struct {
	jobs   []*dlJob
	next   int
	active int
	open   bool     // the panel shows
	lib    *library // the downloaded songs' source, for the Downloaded page
	files  atomic.Pointer[[]dlRecord]
}

// byID returns the job id, or nil.
func (m *dlManager) byID(id int) *dlJob {
	for _, j := range m.jobs {
		if j.id == id {
			return j
		}
	}
	return nil
}

// working is how many songs are queued or downloading, for the button's mark.
func (m *dlManager) working() int {
	n := 0
	for _, j := range m.jobs {
		if j.status == dlQueued || j.status == dlDownloading {
			n++
		}
	}
	return n
}

// jobFor returns the job of a song on its way, or else the last one that
// ended, for a row to mark its download. Nil when it has none.
func (m *dlManager) jobFor(tr *music.Track) *dlJob {
	var ended *dlJob
	for _, j := range m.jobs {
		if j.track.ID != tr.ID || j.track.Source != tr.Source {
			continue
		}
		if j.status == dlQueued || j.status == dlDownloading || j.status == dlPaused {
			return j
		}
		ended = j
	}
	return ended
}

// add queues one song, sharing an existing in-flight job for the same source ID.
func (m *dlManager) add(a *app, j *dlJob) {
	if j.track.Source == "" && j.lib != nil {
		j.track.Source = j.lib.id
	}
	if previous := m.jobFor(&j.track); previous != nil && (previous.status == dlQueued || previous.status == dlDownloading || previous.status == dlPaused) {
		j.skipped = true
		m.report(a, j)
		return
	}
	for _, old := range m.jobs {
		if !sameTrack(&old.track, &j.track) &&
			songFileName(&old.track, "") == songFileName(&j.track, "") &&
			(old.dir == j.dir || old.choosePath && j.choosePath) {
			j.variant = true
		}
	}
	m.next++
	j.id = m.next
	j.status = dlQueued
	j.at = time.Now()
	m.jobs = append(m.jobs, j)
	m.pump(a)
}

// addBatch queues tracks to save into dir, one toast telling of them all.
func (m *dlManager) addBatch(a *app, quality string, tracks []music.Track, dir string) {
	g := &dlGroup{total: len(tracks)}
	for _, tr := range tracks {
		m.add(a, &dlJob{track: tr, dir: dir, lib: a.trackLib(&tr), quality: quality, group: g})
	}
	m.pump(a)
}

// pump starts what is queued, up to dlLimit at once.
func (m *dlManager) pump(a *app) {
	for m.active < dlLimit {
		var j *dlJob
		for _, q := range m.jobs {
			if q.status == dlQueued {
				j = q
				break
			}
		}
		if j == nil {
			return
		}
		j.status = dlDownloading
		m.active++
		go m.run(a, j)
	}
}

// run saves one song, off the main thread, posting what becomes of it.
func (m *dlManager) run(a *app, j *dlJob) {
	if stream := a.localAudio(j.track); stream != nil {
		if path, ok := music.PathOf(stream.URL); ok {
			a.post(func() {
				j.path, j.size, j.done, j.status, j.skipped = path, stream.Size, stream.Size, dlDone, true
				j.sizeVerified = true
				m.settle(a, j)
			})
			return
		}
	}
	if path, size := m.existingTarget(j); path != "" {
		a.post(func() {
			j.path, j.size, j.done, j.status, j.skipped = path, size, size, dlDone, true
			j.sizeVerified = true
			m.settle(a, j)
		})
		return
	}
	var song *player.Song
	var err error
	if j.lib == nil {
		err = music.ErrUnavailable
	} else {
		song, err = a.pl.FindSong(j.lib.src, j.track, j.quality)
	}
	if err != nil {
		a.post(func() { j.status, j.err = dlFailed, err.Error(); m.settle(a, j) })
		return
	}
	path := j.path
	if j.choosePath && path == "" {
		path = saveSongAs(a, downloadFileName(j.track, song.Type, j.variant))
		if path == "" {
			song.Close()
			a.post(func() { j.status = dlCanceled; m.settle(a, j) })
			return
		}
	} else if path == "" {
		path = filepath.Join(j.dir, downloadFileName(j.track, song.Type, j.variant))
		if m.conflictingTarget(j.track, path) {
			path = filepath.Join(j.dir, downloadFileName(j.track, song.Type, true))
		}
	}
	if m.conflictingTarget(j.track, path) {
		song.Close()
		a.post(func() {
			j.status, j.err = dlFailed, T("This file belongs to another song. Choose a different filename.")
			m.settle(a, j)
		})
		return
	}
	expectedSize := song.Size()
	size := expectedSize
	// One already there is left as it is.
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 && m.targetMatches(j.track, path, info.Size()) {
		song.Close()
		a.post(func() {
			j.path, j.size, j.done, j.status = path, info.Size(), info.Size(), dlDone
			j.skipped = true
			j.sizeVerified = true
			m.settle(a, j)
		})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.post(func() {
		j.path, j.size, j.cancel, j.status = path, expectedSize, cancel, dlDownloading
		if j.pausing {
			cancel()
		}
	})
	var last int64
	progress := func(n int64) {
		if n-last >= 128<<10 {
			last = n
			a.post(func() {
				if j.status == dlDownloading {
					j.done = n
				}
			})
		}
	}
	if st := song.Stream(); st != nil {
		// Resume from what a pause left in the part file, if any.
		var from int64
		if info, serr := os.Stat(path + ".part"); serr == nil {
			from = info.Size()
		}
		err = player.SaveStream(ctx, st, path, from, progress)
		song.Close()
	} else {
		err = song.SaveToContext(ctx, path, progress)
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(path)
		if err == nil {
			size = info.Size()
			if size <= 0 {
				err = music.ErrUnavailable
			}
		}
	}
	a.post(func() {
		j.cancel = nil
		switch {
		case errors.Is(err, context.Canceled):
			if j.pausing {
				j.status = dlPaused
			} else {
				j.status = dlCanceled
				os.Remove(path + ".part")
			}
		case err != nil:
			j.status, j.err = dlFailed, err.Error()
		default:
			j.status, j.size, j.done, j.path = dlDone, size, size, path
			j.sizeVerified = true
		}
		m.settle(a, j)
	})
}

// settle ends a job on the main thread: keeps what was saved, tells of a batch
// when its last song lands, and starts the next queued.
func (m *dlManager) settle(a *app, j *dlJob) {
	m.active--
	if j.status == dlPaused {
		m.pump(a)
		return
	}
	if j.status == dlDone {
		m.jobs = slices.DeleteFunc(m.jobs, func(old *dlJob) bool {
			return old != j && old.status == dlDone && sameTrack(&old.track, &j.track)
		})
		m.persist()
	}
	m.report(a, j)
	m.pump(a)
}

func (m *dlManager) report(a *app, j *dlJob) {
	if g := j.group; g != nil {
		g.finished++
		switch {
		case j.skipped:
			g.skipped++
		case j.status == dlDone:
			g.saved++
		case j.status == dlFailed:
			g.failed++
		}
		if g.finished == g.total {
			if g.skipped > 0 {
				a.toast = Tf("Downloaded %d songs, skipped %d (%d failed)", g.saved, g.skipped, g.failed)
			} else if g.failed > 0 {
				a.toast = Tf("Downloaded %d songs (%d failed)", g.saved, g.failed)
			} else {
				a.toast = Tf("Downloaded %d songs", g.saved)
			}
		}
	} else if j.skipped {
		a.toast = Tf("Already saved or queued %s", j.track.Name)
	} else if j.status == dlDone {
		a.toast = Tf("Downloaded %s", j.track.Name)
	}
}

// cancel stops a job for good: one queued is dropped, one downloading is cut
// short and its part removed, one paused gives up its part.
func (m *dlManager) cancel(id int) {
	j := m.byID(id)
	if j == nil {
		return
	}
	switch j.status {
	case dlQueued:
		j.status = dlCanceled
	case dlDownloading:
		j.pausing = false
		if j.cancel != nil {
			j.cancel()
		}
	case dlPaused:
		j.status = dlCanceled
		if j.path != "" {
			os.Remove(j.path + ".part")
		}
	}
}

// pause stops a download but keeps what it has saved, to resume from later.
func (m *dlManager) pause(id int) {
	j := m.byID(id)
	if j == nil || j.status != dlDownloading {
		return
	}
	j.pausing = true
	if j.cancel != nil {
		j.cancel()
	}
}

// resume queues a paused job again, to carry on from its part file.
func (m *dlManager) resume(a *app, id int) {
	j := m.byID(id)
	if j == nil || j.status != dlPaused {
		return
	}
	j.pausing, j.status, j.err = false, dlQueued, ""
	m.pump(a)
}

// retry queues a job that failed or was canceled again.
func (m *dlManager) retry(a *app, id int) {
	j := m.byID(id)
	if j == nil || (j.status != dlFailed && j.status != dlCanceled) {
		return
	}
	j.status, j.done, j.err, j.cancel, j.pausing = dlQueued, 0, "", nil, false
	m.pump(a)
}

// clearFinished hides completed jobs while retaining their saved-file index,
// and drops failed and canceled jobs from the panel.
func (m *dlManager) clearFinished() {
	for _, j := range m.jobs {
		if j.status == dlDone {
			j.hidden = true
		}
	}
	m.jobs = slices.DeleteFunc(m.jobs, func(j *dlJob) bool {
		return j.status == dlFailed || j.status == dlCanceled
	})
	m.persist()
}

// anyFinished reports whether a job has ended, for the panel's Clear.
func (m *dlManager) anyFinished() bool {
	for _, j := range m.jobs {
		if !j.hidden && (j.status == dlDone || j.status == dlFailed || j.status == dlCanceled) {
			return true
		}
	}
	return false
}

// remove hides a completed job without losing its saved-file index.
func (m *dlManager) remove(id int) {
	if j := m.byID(id); j != nil && j.status == dlDone {
		j.hidden = true
	} else {
		m.jobs = slices.DeleteFunc(m.jobs, func(j *dlJob) bool { return j.id == id })
	}
	m.persist()
}

// dlRecord is a song saved, as it is kept on disk.
type dlRecord struct {
	Track        music.Track `json:"track"`
	Path         string      `json:"path"`
	Size         int64       `json:"size"`
	At           time.Time   `json:"at"`
	Hidden       bool        `json:"hidden,omitempty"`
	SizeVerified bool        `json:"sizeVerified,omitempty"`
}

func downloadsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "downloads.json"), nil
}

// persist keeps the songs saved, for the downloads to show again.
func (m *dlManager) persist() {
	var recs []dlRecord
	for _, j := range m.jobs {
		if j.status == dlDone {
			recs = append(recs, dlRecord{Track: j.track, Path: j.path, Size: j.size, At: j.at, Hidden: j.hidden, SizeVerified: j.sizeVerified})
		}
	}
	m.files.Store(&recs)
	path, err := downloadsPath()
	if err != nil {
		return
	}
	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o700)
	os.WriteFile(path, b, 0o600)
}

// load reads the songs saved before, as jobs done.
func (m *dlManager) load() {
	path, err := downloadsPath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var recs []dlRecord
	if json.Unmarshal(b, &recs) != nil {
		return
	}
	m.files.Store(&recs)
	for _, r := range recs {
		m.next++
		m.jobs = append(m.jobs, &dlJob{
			id: m.next, track: r.Track, path: r.Path, size: r.Size, done: r.Size,
			status: dlDone, at: r.At, hidden: r.Hidden, sizeVerified: r.SizeVerified,
		})
	}
}
