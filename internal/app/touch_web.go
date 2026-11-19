//go:build js

package app

import "syscall/js"

func init() {
	allowTouchScroll = func() {
		bridge := js.Global().Get("__mygoTouchScroll")
		if bridge.Type() == js.TypeObject {
			bridge.Call("allow")
		}
	}
}
