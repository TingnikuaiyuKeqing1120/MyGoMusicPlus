//go:build js

package app

import (
	"errors"
	"sync"
	"syscall/js"
	"time"
)

// deviceCalls keeps the answers of the shell's sign-in requests, by the id
// each went out with: the bridge's methods return at once and the answer
// comes back through __mygoDeviceResult, so the HTTP never holds the app's
// thread.
var deviceCalls = struct {
	mu      sync.Mutex
	n       int
	waiting map[int]chan string
}{waiting: map[int]chan string{}}

// useDeviceFlow takes the shell's GitHub sign-in where it offers one, as the
// Android shell does.
func useDeviceFlow() {
	bridge := js.Global().Get("AndroidBridge")
	if !bridge.Truthy() || bridge.Get("githubDeviceStart").Type() != js.TypeFunction {
		return
	}
	js.Global().Set("__mygoDeviceResult", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return nil
		}
		id, body := args[0].Int(), args[1].String()
		deviceCalls.mu.Lock()
		ch := deviceCalls.waiting[id]
		delete(deviceCalls.waiting, id)
		deviceCalls.mu.Unlock()
		if ch != nil {
			ch <- body
		}
		return nil
	}))
	request := func(method string, args ...any) (string, error) {
		deviceCalls.mu.Lock()
		deviceCalls.n++
		id := deviceCalls.n
		ch := make(chan string, 1)
		deviceCalls.waiting[id] = ch
		deviceCalls.mu.Unlock()
		bridge.Call(method, append(args, id)...)
		select {
		case body := <-ch:
			return body, nil
		case <-time.After(30 * time.Second):
			deviceCalls.mu.Lock()
			delete(deviceCalls.waiting, id)
			deviceCalls.mu.Unlock()
			return "", errors.New(T("GitHub didn't answer in time"))
		}
	}
	deviceStart = func(clientID, scope string) (string, error) {
		return request("githubDeviceStart", clientID, scope)
	}
	devicePoll = func(clientID, code string) (string, error) {
		return request("githubDevicePoll", clientID, code)
	}
}