//go:build js

package player

import (
	"encoding/json"
	"syscall/js"

	"mygo-music/internal/music"
)

type webNativePlayback struct{ api js.Value }

func (b *webNativePlayback) cache(action string, limit int64) nativeCacheStatus {
	var s nativeCacheStatus
	json.Unmarshal([]byte(b.api.Call("cache", action, float64(limit)).String()), &s)
	return s
}

func init() {
	makeNativePlayback = func(changed func(nativeState), resolve func(int, int)) nativePlayback {
		api := js.Global().Get("__mygoNativePlayback")
		if !api.Truthy() {
			return nil
		}
		state := js.FuncOf(func(this js.Value, args []js.Value) any {
			var s nativeState
			if len(args) > 0 && json.Unmarshal([]byte(args[0].String()), &s) == nil {
				changed(s)
			}
			return nil
		})
		request := js.FuncOf(func(this js.Value, args []js.Value) any {
			if len(args) > 1 {
				resolve(args[0].Int(), args[1].Int())
			}
			return nil
		})
		api.Call("connect", state, request)
		return &webNativePlayback{api: api}
	}
}

func (b *webNativePlayback) command(c nativeCommand) {
	data, _ := json.Marshal(c)
	b.api.Call("command", string(data))
}

func (b *webNativePlayback) resolve(generation, slot int, info *music.Stream, err error) {
	message := ""
	if err != nil {
		message = errorText(err)
	}
	data, _ := json.Marshal(struct {
		Generation int           `json:"generation"`
		Slot       int           `json:"slot"`
		Stream     *music.Stream `json:"stream,omitempty"`
		Error      string        `json:"error,omitempty"`
	}{generation, slot, info, message})
	b.api.Call("resolve", string(data))
}
