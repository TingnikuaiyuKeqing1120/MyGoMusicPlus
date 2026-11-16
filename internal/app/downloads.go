package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
)

// dlLimit is how many songs download at once.
const dlLimit = 3

// dlJob is one song on its way to a file: where it goes, how far it has come,
// and what became of it.
type dlJob struct {
	id      int
	track   music.Track
	dir     string // the folder to save into, when the path isn't set yet
	path    string // the file it goes to, once known
	status  dlStatus
	done    int64
	size    int64
	err     string
	cancel  context.CancelFunc
	song    *player.Song // found already, for a single download, or nil to find
	lib     *library
	quality string
	group   *dlGroup // the batch it is one of, or nil for a single download
	at      time.Time
}

// dlGroup is a batch downloaded together, whose end one toast tells of.
type dlGroup struct {
	total, finished, saved, failed int
}

type dlManager struct {
	jobs   []*dlJob
	next   int
	active int
	open   bool // the panel shows
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
		if j.status == dlQueued || j.status == dlDownloading {
			return j
		}
		ended = j
	}
	return ended
}

// add queues one song to save to path, found already as song.
func (m *dlManager) add(a *app, j *dlJob) {
	m.next++
	j.id = m.next
	j.status = dlQueued
	j.at = time.Now()
	m.jobs = append(m.jobs, j)
	m.pump(a)
}

// addBatch queues tracks to save into dir, one toast telling of them all.
func (m *dlManager) addBatch(a *app, lib *library, quality string, tracks []music.Track, dir string) {
	g := &dlGroup{total: len(tracks)}
	for _, tr := range tracks {
		m.next++
		m.jobs = append(m.jobs, &dlJob{
			id: m.next, track: tr, dir: dir, status: dlQueued,
			lib: lib, quality: quality, group: g, at: time.Now(),
		})
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
	song := j.song
	if song == nil {
		var err error
		song, err = a.pl.FindSong(j.lib.src, j.track, j.quality)
		if err != nil {
			a.post(func() { j.status, j.err = dlFailed, err.Error(); m.settle(a, j) })
			return
		}
	}
	path := j.path
	if path == "" {
		path = filepath.Join(j.dir, songFileName(&j.track, song.Type))
	}
	size := song.Size()
	// One already there is left as it is.
	if info, err := os.Stat(path); err == nil {
		song.Close()
		a.post(func() {
			j.path, j.size, j.done, j.status = path, info.Size(), info.Size(), dlDone
			m.settle(a, j)
		})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.post(func() { j.path, j.size, j.cancel, j.status = path, size, cancel, dlDownloading })
	var last int64
	err := song.SaveToContext(ctx, path, func(n int64) {
		if n-last >= 128<<10 {
			last = n
			a.post(func() {
				if j.status == dlDownloading {
					j.done = n
				}
			})
		}
	})
	a.post(func() {
		j.cancel = nil
		switch {
		case errors.Is(err, context.Canceled):
			j.status = dlCanceled
		case err != nil:
			j.status, j.err = dlFailed, err.Error()
		default:
			j.status, j.done, j.path = dlDone, size, path
		}
		m.settle(a, j)
	})
}

// settle ends a job on the main thread: keeps what was saved, tells of a batch
// when its last song lands, and starts the next queued.
func (m *dlManager) settle(a *app, j *dlJob) {
	m.active--
	if j.status == dlDone {
		m.persist()
	}
	if g := j.group; g != nil {
		g.finished++
		switch j.status {
		case dlDone:
			g.saved++
		case dlFailed:
			g.failed++
		}
		if g.finished == g.total {
			if g.failed > 0 {
				a.toast = Tf("Downloaded %d songs (%d failed)", g.saved, g.failed)
			} else {
				a.toast = Tf("Downloaded %d songs", g.saved)
			}
		}
	} else if j.status == dlDone {
		a.toast = Tf("Downloaded %s", j.track.Name)
	}
	m.pump(a)
}

// cancel stops a job: one queued is dropped, one downloading is cut short.
func (m *dlManager) cancel(id int) {
	j := m.byID(id)
	if j == nil {
		return
	}
	switch j.status {
	case dlQueued:
		j.status = dlCanceled
	case dlDownloading:
		if j.cancel != nil {
			j.cancel()
		}
	}
}

// retry queues a job that failed or was canceled again.
func (m *dlManager) retry(a *app, id int) {
	j := m.byID(id)
	if j == nil || (j.status != dlFailed && j.status != dlCanceled) {
		return
	}
	j.status, j.done, j.err, j.song, j.cancel = dlQueued, 0, "", nil, nil
	m.pump(a)
}

// clearFinished drops the jobs that ended, keeping what is on its way, and
// keeps the saved ones on disk as they are now.
func (m *dlManager) clearFinished() {
	m.jobs = slices.DeleteFunc(m.jobs, func(j *dlJob) bool {
		return j.status == dlDone || j.status == dlFailed || j.status == dlCanceled
	})
	m.persist()
}

// anyFinished reports whether a job has ended, for the panel's Clear.
func (m *dlManager) anyFinished() bool {
	for _, j := range m.jobs {
		if j.status == dlDone || j.status == dlFailed || j.status == dlCanceled {
			return true
		}
	}
	return false
}

// remove drops one job from the list, keeping the rest on disk as they are.
func (m *dlManager) remove(id int) {
	m.jobs = slices.DeleteFunc(m.jobs, func(j *dlJob) bool { return j.id == id })
	m.persist()
}

// dlRecord is a song saved, as it is kept on disk.
type dlRecord struct {
	Track music.Track `json:"track"`
	Path  string      `json:"path"`
	Size  int64       `json:"size"`
	At    time.Time   `json:"at"`
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
	path, err := downloadsPath()
	if err != nil {
		return
	}
	var recs []dlRecord
	for _, j := range m.jobs {
		if j.status == dlDone {
			recs = append(recs, dlRecord{Track: j.track, Path: j.path, Size: j.size, At: j.at})
		}
	}
	if len(recs) > 1000 {
		recs = recs[len(recs)-1000:]
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
	for _, r := range recs {
		m.next++
		m.jobs = append(m.jobs, &dlJob{
			id: m.next, track: r.Track, path: r.Path, size: r.Size, done: r.Size,
			status: dlDone, at: r.At,
		})
	}
}