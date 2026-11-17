//go:build js

package app

import "syscall/js"

// webBackBridge gives the shell's back button to the app: the Android shell
// calls __mygoBack and keeps the button only while the app answers it took a
// step, so before the app boots, or at its root, the button leaves as usual.
func webBackBridge(a *app) {
	cb := js.FuncOf(func(this js.Value, args []js.Value) any {
		return a.shellBack()
	})
	js.Global().Set("__mygoBack", cb)
}