//go:build js

package plugin

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"syscall/js"
)

// GD's webpage endpoints omit CORS headers. Android performs these public
// GETs on the device after the plugin host has checked network permissions.
func pluginTransport() http.RoundTripper { return gdWebTransport{} }

type gdWebTransport struct{}

func (gdWebTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	bridge := js.Global().Get("AndroidBridge")
	u := req.URL
	if req.Method != http.MethodGet || u.Scheme != "https" || u.User != nil || u.Port() != "" ||
		(u.Host != "music.gdstudio.org" && u.Host != "music.gdstudio.xyz") ||
		(u.Path != "/api.php" && u.Path != "/time" && u.Path != "/js/player.js") ||
		!bridge.Truthy() || bridge.Get("hasGDWebLoader").Type() != js.TypeFunction {
		return http.DefaultTransport.RoundTrip(req)
	}
	forward := req.Clone(req.Context())
	forward.URL, _ = url.Parse("https://appassets.androidplatform.net/native-gd/" + base64.RawURLEncoding.EncodeToString([]byte(u.String())))
	forward.Host = ""
	res, err := http.DefaultTransport.RoundTrip(forward)
	if res != nil {
		res.Request = req
	}
	return res, err
}
