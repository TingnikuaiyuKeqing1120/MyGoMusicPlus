//go:build !js

package plugin

import "net/http"

func pluginTransport() http.RoundTripper { return nil }
