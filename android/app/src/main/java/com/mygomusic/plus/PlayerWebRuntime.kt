package com.mygomusic.plus

import android.annotation.SuppressLint
import android.os.Handler
import android.os.Looper
import android.view.ViewGroup
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.webkit.WebViewAssetLoader
import java.lang.ref.WeakReference

// The service retains the source/plugin runtime when the activity finishes.
// Application context and detached clients avoid retaining the old activity.
object PlayerWebRuntime {
    private val main = Handler(Looper.getMainLooper())
    private var web: WebView? = null
    private var owner = WeakReference<MainActivity>(null)
    private var assetLoader: WebViewAssetLoader? = null
    private var files: NativeAudioFiles? = null
    var foreground = false
        private set

    @SuppressLint("SetJavaScriptEnabled")
    fun obtain(activity: MainActivity): WebView {
        owner = WeakReference(activity)
        foreground = true
        val existing = web
        if (existing != null) {
            (existing.parent as? ViewGroup)?.removeView(existing)
            return existing
        }
        val context = activity.applicationContext
        files = NativeAudioFiles(context)
        return WebView(context).also { web = it }
    }

    fun audioFiles(): NativeAudioFiles = checkNotNull(files)
    fun useAssets(loader: WebViewAssetLoader) { assetLoader = loader }
    fun pickMusicFolder() { owner.get()?.chooseMusicFolder() }

    fun visibility(activity: MainActivity, visible: Boolean) {
        if (owner.get() !== activity) return
        foreground = visible
        if (visible) NativePlayback.service?.publish(forceCache = true)
    }

    fun evaluate(script: String) {
        main.post { web?.evaluateJavascript(script, null) }
    }

    fun detach(activity: MainActivity) {
        if (owner.get() !== activity) return
        owner.clear()
        foreground = false
        web?.let { view ->
            (view.parent as? ViewGroup)?.removeView(view)
            view.webChromeClient = WebChromeClient()
            view.setDownloadListener(null)
            view.webViewClient = object : WebViewClient() {
                override fun shouldInterceptRequest(v: WebView, request: WebResourceRequest): WebResourceResponse? =
                    assetLoader?.shouldInterceptRequest(request.url)
                override fun shouldOverrideUrlLoading(v: WebView, request: WebResourceRequest): Boolean = true
            }
        }
        releaseIfIdle()
    }

    fun releaseIfIdle() {
        if (owner.get() != null || NativePlayback.service != null) return
        web?.destroy()
        web = null
        assetLoader = null
        files?.close()
        files = null
        WebLink.device = null
        WebLink.install = null
    }
}
