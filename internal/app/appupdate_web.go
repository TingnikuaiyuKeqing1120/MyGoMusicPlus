//go:build js

package app

import "syscall/js"

// useAppInstall takes the shell's APK installer where it offers one, as the
// Android shell does, and hears how the download and install go through
// __mygoInstallState.
func useAppInstall(a *app) {
	bridge := js.Global().Get("AndroidBridge")
	if !bridge.Truthy() || bridge.Get("installApk").Type() != js.TypeFunction {
		return
	}
	installApk = func(url string) { bridge.Call("installApk", url) }
	js.Global().Set("__mygoInstallState", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return nil
		}
		state := args[0].String()
		a.post(func() { a.appUpd.install = state })
		return nil
	}))
}