package com.mygomusic.plus

// WebLink is the seam between the native shell and the wasm app running in the
// WebView: the shell drives the app's recorded mediaSession handlers, and the
// app (via Bridge) drives the shell's notification / audio focus.
object WebLink {
    @Volatile
    var control: ((String) -> Unit)? = null

    fun send(action: String) {
        control?.invoke(action)
    }
}