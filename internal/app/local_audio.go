package app

import (
	"crypto/sha256"
	"fmt"
	"mygo-music/internal/local"
	"mygo-music/internal/music"
	"os"
	"path/filepath"
	"strings"
)

func downloadFileName(track music.Track, ext string, variant bool) string {
	if !variant {
		return songFileName(&track, ext)
	}
	digest := sha256.Sum256([]byte(track.Source + "\x00" + track.ID))
	name := strings.TrimSuffix(songFileName(&track, ext), "."+ext)
	return fmt.Sprintf("%s [%x].%s", name, digest[:5], ext)
}

func (m *dlManager) conflictingTarget(track music.Track, path string) bool {
	if records := m.files.Load(); records != nil {
		for _, record := range *records {
			if record.Path == path && !sameTrack(&track, &record.Track) && !music.SameRecording(track, record.Track) {
				return true
			}
		}
	}
	return false
}

func (m *dlManager) targetMatches(track music.Track, path string, size int64) bool {
	known := false
	if records := m.files.Load(); records != nil {
		for _, record := range *records {
			if record.Path != path {
				continue
			}
			known = true
			if (sameTrack(&track, &record.Track) || music.SameRecording(track, record.Track)) && (!record.SizeVerified || record.Size == size) {
				return true
			}
		}
	}
	return !known
}

// existingTarget also recognizes older downloads whose index has been cleared,
// across supported extensions, before contacting the source for a URL.
func (m *dlManager) existingTarget(job *dlJob) (string, int64) {
	paths := []string{job.path}
	if job.path == "" {
		dir := job.dir
		if job.choosePath {
			dir = downloadDir()
		}
		if dir == "" {
			return "", 0
		}
		paths = nil
		for _, ext := range []string{"flac", "wav", "m4a", "mp3", "ogg", "aac"} {
			paths = append(paths, filepath.Join(dir, downloadFileName(job.track, ext, job.variant)))
		}
	}
	for _, path := range paths {
		if strings.EqualFold(filepath.Ext(path), ".part") {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 && m.targetMatches(job.track, path, info.Size()) {
			return path, info.Size()
		}
	}
	return "", 0
}

// localAudio is called on playback and download workers. Saved-file snapshots
// and the local library's immutable catalog avoid reading the UI's job state.
func (a *app) localAudio(track music.Track) *music.Stream {
	if track.Source == local.ID || track.Source == downloadedID {
		return nil
	}
	if stream := a.downloads.localAudio(track); stream != nil {
		return stream
	}
	if a.local != nil {
		return a.local.Match(track)
	}
	return nil
}

func (m *dlManager) localAudio(track music.Track) *music.Stream {
	records := m.files.Load()
	if records == nil {
		return nil
	}
	open := func(record dlRecord) *music.Stream {
		stream, err := (dlSource{}).Stream(record.Path, "")
		if err != nil || record.SizeVerified && stream.Size != record.Size {
			return nil
		}
		return stream
	}
	for _, record := range *records {
		if sameTrack(&track, &record.Track) {
			if stream := open(record); stream != nil {
				return stream
			}
		}
	}
	var found *music.Stream
	for _, record := range *records {
		if !music.SameRecording(track, record.Track) {
			continue
		}
		if stream := open(record); stream != nil {
			if found != nil && found.URL != stream.URL {
				return nil
			}
			found = stream
		}
	}
	return found
}
