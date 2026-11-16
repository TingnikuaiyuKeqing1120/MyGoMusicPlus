package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"mygo-music/internal/music"
)

// downloadedApp is the Downloaded page of two songs saved to files, with the
// file manager and Trash captured and the delete confirmation answered yes.
func downloadedApp(t *testing.T) (*app, *ui.Tester, chan func(), *[]string, *[]string) {
	t.Helper()
	dir := t.TempDir()
	a := newTestApp()
	ch := posts(a)
	var revealed, trashed []string
	wasReveal, wasTrash, wasConfirm := revealFile, trashFile, confirmDeleteDownload
	revealFile = func(p string) { revealed = append(revealed, p) }
	trashFile = func(p string) error { trashed = append(trashed, p); return nil }
	confirmDeleteDownload = func(a *app, message string, yes func()) { yes() }
	t.Cleanup(func() { revealFile, trashFile, confirmDeleteDownload = wasReveal, wasTrash, wasConfirm })

	for _, s := range []struct{ id, name, file string }{
		{"1", "宝船", "a.mp3"}, {"2", "大海", "b.mp3"},
	} {
		path := filepath.Join(dir, s.file)
		if err := os.WriteFile(path, []byte("the song"), 0o600); err != nil {
			t.Fatal(err)
		}
		a.downloads.next++
		a.downloads.jobs = append(a.downloads.jobs, &dlJob{
			id: a.downloads.next, track: song(s.id, s.name, "DOUDOU"), path: path,
			size: 1 << 20, done: 1 << 20, status: dlDone, at: time.Now(),
		})
	}
	a.route = route{page: pageDownloaded, src: "fake"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	return a, tt, ch, &revealed, &trashed
}

// The Downloaded page lists the songs saved to files, and a click plays one
// from its file, under the downloaded source.
func TestDownloadedPageListsAndPlays(t *testing.T) {
	a, tt, _, _, _ := downloadedApp(t)
	if !tt.HasText("宝船") || !tt.HasText("大海") {
		t.Fatalf("the page doesn't list the saved songs: %q", tt.Texts())
	}
	if err := tt.Click("宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	q := a.pl.Queue()
	if len(q) != 2 {
		t.Fatalf("queued %d songs, want the page's two", len(q))
	}
	if q[0].Source != downloadedID || q[0].ID != a.downloads.savedJobs()[0].path {
		t.Errorf("playing %+v, want the saved file under the downloaded source", q[0])
	}
}

// The sidebar's Downloaded entry opens the page of saved songs.
func TestDownloadedPageOpensFromSidebar(t *testing.T) {
	a, tt, _, _, _ := downloadedApp(t)
	a.route = route{page: pageHome, src: "fake"}
	tt.Frame()
	if err := tt.Click("Downloaded"); err != nil {
		t.Fatalf("%v; the sidebar shows %q", err, tt.Texts())
	}
	tt.Frame()
	if a.route.page != pageDownloaded {
		t.Errorf("opened page %v, want the Downloaded page", a.route.page)
	}
	if !tt.HasText("宝船") {
		t.Errorf("the page doesn't list the saved songs: %q", tt.Texts())
	}
}

// A saved song's row shows its file in the file manager.
func TestDownloadedPageReveal(t *testing.T) {
	a, tt, _, revealed, _ := downloadedApp(t)
	if err := tt.Click("Show in Folder"); err != nil {
		t.Fatalf("%v; the page shows %q", err, tt.Texts())
	}
	if len(*revealed) != 1 || (*revealed)[0] != a.downloads.savedJobs()[0].path {
		t.Errorf("revealed %v, want the first song's file", *revealed)
	}
}

// Deleting a saved song moves its file to the Trash and forgets it.
func TestDownloadedPageDelete(t *testing.T) {
	a, tt, ch, _, trashed := downloadedApp(t)
	path := a.downloads.savedJobs()[0].path
	if err := tt.Click("Delete"); err != nil {
		t.Fatalf("%v; the page shows %q", err, tt.Texts())
	}
	until(t, ch, func() bool { return a.toast == "Deleted 1 songs" })
	if len(*trashed) != 1 || (*trashed)[0] != path {
		t.Errorf("trashed %v, want the song's file %s", *trashed, path)
	}
	if len(a.downloads.savedJobs()) != 1 {
		t.Errorf("left %d saved songs, want one", len(a.downloads.savedJobs()))
	}
}

// Choosing several saved songs deletes them all at once.
func TestDownloadedPageBatchDelete(t *testing.T) {
	a, tt, ch, _, trashed := downloadedApp(t)
	if err := tt.ClickWith(ui.Cmd, "宝船"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.ClickWith(ui.Cmd, "大海"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Delete Selected"); err != nil {
		t.Fatalf("%v; the page shows %q", err, tt.Texts())
	}
	until(t, ch, func() bool { return a.toast == "Deleted 2 songs" })
	if len(*trashed) != 2 {
		t.Errorf("trashed %v, want both files", *trashed)
	}
	if len(a.downloads.savedJobs()) != 0 {
		t.Errorf("left %d saved songs, want none", len(a.downloads.savedJobs()))
	}
}

// The downloaded source answers a saved song's file itself, so it plays from
// disk without the network.
func TestDownloadedSourceStreamsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "song.mp3")
	os.WriteFile(path, []byte("the song"), 0o600)
	st, err := dlSource{}.Stream(path, "high")
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := music.PathOf(st.URL); !ok || got != path {
		t.Errorf("stream URL %q, want the file %s", st.URL, path)
	}
	if st.Type != "mp3" || st.Size != int64(len("the song")) {
		t.Errorf("stream %+v, want an mp3 of the file's size", st)
	}
}