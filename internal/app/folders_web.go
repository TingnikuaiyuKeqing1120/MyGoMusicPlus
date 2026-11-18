//go:build js

package app

import (
	"errors"
	"syscall/js"
)

func chooseMusicFolders() ([]string, error) {
	pick := js.Global().Get("__mygoPickMusicFolder")
	if pick.Type() != js.TypeFunction {
		return nil, errors.New(T("Folder selection is unavailable in this browser"))
	}
	type result struct{ path, message string }
	finished := make(chan result, 1)
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		finished <- result{args[0].String(), args[1].String()}
		return nil
	})
	defer callback.Release()
	pick.Invoke(callback)
	res := <-finished
	if res.message != "" {
		return nil, errors.New(T(res.message))
	}
	if res.path == "" {
		return nil, nil
	}
	return []string{res.path}, nil
}
