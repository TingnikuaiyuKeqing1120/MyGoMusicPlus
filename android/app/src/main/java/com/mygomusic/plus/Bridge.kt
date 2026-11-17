package com.mygomusic.plus

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.webkit.JavascriptInterface

// Bridge is the JS->native surface the web media-bridge calls. It forwards
// playback metadata/state/position to PlaybackService (notification, lock
// screen, audio focus) and offers share / open-external.
class Bridge(private val ctx: Context) {

    private fun toService(key: String, value: String) {
        val i = Intent(ctx, PlaybackService::class.java).putExtra(key, value)
        ctx.startForegroundService(i)
    }

    @JavascriptInterface
    fun onMetadata(json: String) = toService("meta", json)

    @JavascriptInterface
    fun onPlaybackState(playing: String) = toService("playing", playing)

    @JavascriptInterface
    fun onPosition(json: String) = toService("pos", json)

    @JavascriptInterface
    fun openExternal(url: String) {
        ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    @JavascriptInterface
    fun share(text: String) {
        val i = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_TEXT, text)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        ctx.startActivity(Intent.createChooser(i, null).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    @JavascriptInterface
    fun log(msg: String) = android.util.Log.i("MyGoPlus", msg)
}