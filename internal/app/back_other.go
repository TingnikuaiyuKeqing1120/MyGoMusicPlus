//go:build !js

package app

// webBackBridge is the shell's; elsewhere the window has its own back.
func webBackBridge(a *app) {}