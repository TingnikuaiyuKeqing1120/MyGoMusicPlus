// Media bridge between the web app and a native shell (Android).
//
// The app drives navigator.mediaSession (metadata, playbackState, position,
// action handlers). This wraps those so a native shell can show a media
// notification / lock-screen controls, and so the shell can drive playback
// back (audio-focus loss, headset keys) through the recorded handlers.
// With no native bridge present (plain browser) everything is a no-op.
(function () {
  "use strict";
  const ms = navigator.mediaSession;
  if (!ms) return;
  const bridge = () => globalThis.AndroidBridge || null;
  const send = (fn, arg) => { const b = bridge(); if (b && b[fn]) { try { b[fn](arg); } catch (e) {} } };

  const handlers = {};
  globalThis.__mediaHandlers = handlers;
  const origSetAction = ms.setActionHandler.bind(ms);
  ms.setActionHandler = function (name, fn) {
    if (fn) handlers[name] = fn; else delete handlers[name];
    return origSetAction(name, fn);
  };
  // Shell -> web transport controls.
  globalThis.__mediaControl = function (name) {
    const h = handlers[name];
    if (h) { try { h({ action: name }); } catch (e) {} }
  };

  let meta = null;
  Object.defineProperty(ms, "metadata", {
    configurable: true,
    get() { return meta; },
    set(v) {
      meta = v;
      if (!v) { send("onMetadata", ""); return; }
      send("onMetadata", JSON.stringify({
        title: v.title || "", artist: v.artist || "", album: v.album || "",
        artwork: (v.artwork || []).map((a) => a.src),
      }));
    },
  });

  let state = "none";
  Object.defineProperty(ms, "playbackState", {
    configurable: true,
    get() { return state; },
    set(v) { state = v; send("onPlaybackState", v === "playing" ? "1" : "0"); },
  });

  const origPos = ms.setPositionState && ms.setPositionState.bind(ms);
  if (origPos) {
    ms.setPositionState = function (s) {
      send("onPosition", JSON.stringify({
        position: (s && s.position) || 0, duration: (s && s.duration) || 0,
        rate: (s && s.playbackRate) || 1,
      }));
      return origPos(s);
    };
  }
})();