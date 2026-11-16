package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"mygo-music/internal/music"
)

// Stream is where a song not kept in the cache streams from, or nil for one
// that is, for a download manager to save it resumable.
func (s *Song) Stream() *music.Stream { return s.st }

// SaveStream saves the stream st to target, from the byte from on, so a pause
// can resume: it writes target+".part", appending from from, and asks the
// source for the rest with a Range request. Once all of it is in, the part
// becomes target. A cut-short save leaves the part as it is, to resume from;
// the caller removes it to give up. progress tells the absolute bytes in.
func SaveStream(ctx context.Context, st *music.Stream, target string, from int64, progress func(int64)) error {
	part := target + ".part"
	var f *os.File
	var err error
	if from > 0 {
		f, err = os.OpenFile(part, os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			return err
		}
		if _, err = f.Seek(from, io.SeekStart); err != nil {
			f.Close()
			return err
		}
	} else {
		f, err = os.Create(part)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", st.URL, nil)
	if err != nil {
		f.Close()
		return err
	}
	req.Header.Set("User-Agent", music.Agent)
	for k, v := range st.Headers {
		req.Header.Set(k, v)
	}
	if from > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", from))
	}
	res, err := music.HeadersStay(http.DefaultClient, req.URL.Hostname(), st.Headers).Do(req)
	if err != nil {
		f.Close()
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusPartialContent:
	case res.StatusCode == http.StatusOK && from > 0:
		// The source ignored the Range: start over from the beginning.
		if _, err = f.Seek(0, io.SeekStart); err == nil {
			err = f.Truncate(0)
		}
		from = 0
	case res.StatusCode == http.StatusOK:
	default:
		f.Close()
		return errors.New(res.Status)
	}
	if err != nil {
		f.Close()
		return err
	}
	w := &progressWriter{w: f, ctx: ctx, cb: func(n int64) {
		if progress != nil {
			progress(from + n)
		}
	}}
	_, err = io.Copy(w, res.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(part, target)
}