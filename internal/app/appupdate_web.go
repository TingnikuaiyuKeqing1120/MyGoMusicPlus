//go:build js

package app

import "syscall/js"

// useAppInstall takes the shell's APK installer where it offers one, as the
// Android shell does, and hears how the download and install go through
// __mygoInstallState.
func useAppInstall(a *app) {
	bridge := js.Global().Get("AndroidBridge")
	if !bridge.Truthy() || bridge.Get("downloadUpdateApk").Type() != js.TypeFunction {
		return
	}
	downloadApk = func(rel appRelease, mirror bool) {
		bridge.Call("downloadUpdateApk", rel.Tag, rel.APK, rel.Digest, rel.Size, mirror)
	}
	reopenApkInstaller = func() { bridge.Call("openUpdateInstaller") }
	js.Global().Set("__mygoInstallState", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return nil
		}
		state := args[0].String()
		a.post(func() { a.appUpd.restoreInstallState(state) })
		return nil
	}))
	a.appUpd.restoreInstallState(bridge.Call("updateApkState").String())
}
