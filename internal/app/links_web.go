//go:build js

package app

import "syscall/js"

// webLinkBridge takes the links of the app's scheme the shell hands over, as
// the Android shell does when the browser opens one: it sets __mygoLink and
// dispatches "mygo-link". One handed over before the app booted is kept for
// when its window is made; later ones open as they come.
func webLinkBridge(a *app) {
	take := func() string {
		v := js.Global().Get("__mygoLink")
		if v.Type() != js.TypeString {
			return ""
		}
		js.Global().Set("__mygoLink", js.Undefined())
		return v.String()
	}
	if link := take(); link != "" {
		a.links = append(a.links, link)
	}
	cb := js.FuncOf(func(this js.Value, args []js.Value) any {
		a.post(func() {
			if link := take(); link != "" {
				a.openLink(link)
			}
		})
		return nil
	})
	js.Global().Call("addEventListener", "mygo-link", cb)
}