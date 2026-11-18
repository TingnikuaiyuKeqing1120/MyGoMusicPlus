package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-music/internal/profileimport"
)

// Clicking Import in Sync settings schedules exactly one restart only after confirmation.
func TestImportFromOriginalInSettings(t *testing.T) {
	p := profileimport.Desktop(t.TempDir(), "MyGo Music Plus Dev")
	if err := os.Mkdir(p.Source, 0700); err != nil {
		t.Fatal(err)
	}
	oldProfiles, oldConfirm, oldRestart := importProfiles, confirmProfileImport, restartForImport
	t.Cleanup(func() { importProfiles, confirmProfileImport, restartForImport = oldProfiles, oldConfirm, oldRestart })
	importProfiles = func() (profileimport.Profiles, bool) { return p, true }
	var accept bool
	confirmProfileImport = func(_ *mygo.Window, answer func(bool)) { answer(accept) }
	restarts := 0
	restartForImport = func() { restarts++ }
	a := newTestApp()
	ch := posts(a)
	a.settingsPage = "sync"
	tester := ui.NewTester(a.settingsView, 780, 640)
	settled(tester)
	click := func() {
		t.Helper()
		if err := tester.Click(T("Import from Original")); err != nil {
			t.Fatal(err)
		}
		tester.Frame()
	}
	click()
	if len(ch) != 0 || restarts != 0 {
		t.Fatal("cancel requested an import")
	}
	accept = true
	click()
	runPost(t, ch)
	if restarts != 1 {
		t.Fatalf("restarts = %d", restarts)
	}
	if _, err := os.Stat(filepath.Join(p.Backups, "requested")); err != nil {
		t.Fatal(err)
	}
}
