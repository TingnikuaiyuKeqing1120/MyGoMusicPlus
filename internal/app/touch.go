package app

import "github.com/egoist/mygo/ui"

var allowTouchScroll = func() {}

// Only scroll containers opt into vertical panning. MyGo v0.9 observers omit
// the pointer device, so the web adapter checks for a primary touch itself.
func touchScrollInput(ev ui.InputEvent) {
	if ev.Kind == ui.InputPointerDown {
		allowTouchScroll()
	}
}
