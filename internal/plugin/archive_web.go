//go:build js

package plugin

import (
	"encoding/base64"
	"syscall/js"
)

// GitHub's archive hosts don't allow WebView fetches. The Android shell can
// stream the public archive back to the app over its own origin.
func archiveRequestURL(raw string) string {
	bridge := js.Global().Get("AndroidBridge")
	if !bridge.Truthy() || bridge.Get("hasGitHubArchiveLoader").Type() != js.TypeFunction {
		return raw
	}
	return "https://appassets.androidplatform.net/native-github/" + base64.RawURLEncoding.EncodeToString([]byte(raw))
}
