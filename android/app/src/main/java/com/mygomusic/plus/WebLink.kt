package com.mygomusic.plus

// WebLink is the seam between the native shell and the wasm app running in the
// WebView: the shell drives the app's recorded mediaSession handlers, and the
// app (via Bridge) drives the shell's notification / audio focus.
object WebLink {
    @Volatile
    var control: ((String) -> Unit)? = null

    // device answers the app's GitHub sign-in requests, by the id each went
    // out with; install reports progress and the version of a saved APK.
    @Volatile
    var device: ((Int, String) -> Unit)? = null

    @Volatile
    var install: ((String) -> Unit)? = null

    fun send(action: String) {
        control?.invoke(action)
    }
}
