//go:build !js

package app

import "github.com/egoist/mygo"

func chooseMusicFolders() ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:             T("Add a Music Folder"),
		ButtonLabel:       T("Add"),
		Directory:         true,
		Multiple:          true,
		CreateDirectories: false,
	})
}
