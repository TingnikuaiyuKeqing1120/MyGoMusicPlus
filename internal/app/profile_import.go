package app

import (
	"errors"
	"io/fs"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"mygo-music/internal/profileimport"
)

// The development app imports into its own profile, just as it plays from it.
var importProfiles = func() (profileimport.Profiles, bool) {
	name := mygo.App.Name()
	if onWeb || (name != "MyGo Music Plus" && name != "MyGo Music Plus Dev") {
		return profileimport.Profiles{}, false
	}
	dir, err := os.UserConfigDir()
	return profileimport.Desktop(dir, name), err == nil
}

var restartForImport = func() { mygo.App.Relaunch() }

var confirmProfileImport = func(parent *mygo.Window, answer func(bool)) {
	go func() {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: parent, Type: mygo.MessageWarning, Message: T("Import from MyGo Music?"),
			Detail:  T("Quit the original player first. Plus will restart, back up its current profile and import the original library, preferences, plugins and their stored data. Matching data is replaced."),
			Buttons: []string{T("Cancel"), T("Import and Restart")}, DefaultButton: 0,
		})
		answer(err == nil && result.Button == 1)
	}()
}

func (a *app) importSettings(c *ui.Context, row settingsRow) {
	profiles, ok := importProfiles()
	if !ok {
		return
	}
	row(T("Import from MyGo Music"), T("Copy this computer's original player profile into this edition of Plus."), func() {
		ui.Button(c.Key("import-upstream"), T("Import from Original")).Disabled(a.syncer.busy).OnClick(func() {
			confirmProfileImport(a.settingsWin, func(yes bool) {
				if !yes {
					return
				}
				a.post(func() {
					if err := profiles.Request(); err != nil {
						detail := err.Error()
						if errors.Is(err, fs.ErrNotExist) {
							detail = Tf("No original player profile found at %s", profiles.Source)
						}
						showImportMessage(a.settingsWin, T("Import could not start"), detail, mygo.MessageError)
						return
					}
					restartForImport()
				})
			})
		})
	})
}

func showImportMessage(parent *mygo.Window, title, detail string, kind mygo.MessageType) {
	go func() {
		_, _ = mygo.Dialog.Message(mygo.MessageOptions{Parent: parent, Type: kind, Message: title, Detail: detail, Buttons: []string{T("OK")}})
	}()
}

// Do not open a profile whose pre-import backup could not be restored.
func importRecoveryError(err error) error {
	mygo.App.WhenReady(func() {
		go func() {
			_, _ = mygo.Dialog.Message(mygo.MessageOptions{Type: mygo.MessageError, Message: T("Import failed"), Detail: err.Error()})
			mygo.App.Quit()
		}()
	})
	return mygo.App.Run()
}
