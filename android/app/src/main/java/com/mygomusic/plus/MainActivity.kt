package com.mygomusic.plus

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast

// MainActivity hosts the MyGo web build (wasm) in a WebView. The web shell's
// fs-bridge keeps data in OPFS; media-bridge forwards MediaSession to
// PlaybackService so playback survives backgrounding.
class MainActivity : Activity() {

    private lateinit var web: WebView
    private var fileChooser: ValueCallback<Array<Uri>>? = null

    companion object {
        private const val REQ_FILE = 41
        private const val REQ_SAVE = 42
        private var saveBytes: ByteArray? = null
        private var saveName: String? = null
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        web = WebView(this)
        setContentView(web)
        with(web.settings) {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            mediaPlaybackRequiresUserGesture = false
            allowFileAccess = true
            allowContentAccess = true
            mixedContentMode = android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW
        }
        web.addJavascriptInterface(Bridge(this), "AndroidBridge")
        WebLink.control = { action ->
            runOnUiThread {
                web.evaluateJavascript("globalThis.__mediaControl && globalThis.__mediaControl('$action')", null)
            }
        }
        web.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(v: WebView, req: WebResourceRequest): Boolean {
                val u = req.url.toString()
                if (u.startsWith("file:///android_asset/")) return false
                startActivity(Intent(Intent.ACTION_VIEW, req.url))
                return true
            }

            override fun onPageFinished(v: WebView, url: String) {
                deliverLink(intent)
            }
        }
        web.webChromeClient = object : WebChromeClient() {
            override fun onShowFileChooser(
                v: WebView, cb: ValueCallback<Array<Uri>>, params: FileChooserParams
            ): Boolean {
                fileChooser?.onReceiveValue(null)
                fileChooser = cb
                val i = Intent(Intent.ACTION_OPEN_DOCUMENT).apply {
                    addCategory(Intent.CATEGORY_OPENABLE)
                    type = "*/*"
                    putExtra(Intent.EXTRA_ALLOW_MULTIPLE, true)
                }
                startActivityForResult(i, REQ_FILE)
                return true
            }
        }
        web.setDownloadListener { url, _, contentDisposition, mime, _ ->
            // Downloads from the web app: hand the bytes to the system via SAF.
            Thread {
                try {
                    val bytes = java.net.URL(url).openStream().use { it.readBytes() }
                    saveBytes = bytes
                    saveName = android.webkit.URLUtil.guessFileName(url, contentDisposition, mime)
                    runOnUiThread {
                        val i = Intent(Intent.ACTION_CREATE_DOCUMENT).apply {
                            addCategory(Intent.CATEGORY_OPENABLE)
                            type = mime ?: "application/octet-stream"
                            putExtra(Intent.EXTRA_TITLE, saveName)
                        }
                        startActivityForResult(i, REQ_SAVE)
                    }
                } catch (e: Exception) {
                    runOnUiThread { Toast.makeText(this, "download failed: $e", Toast.LENGTH_SHORT).show() }
                }
            }.start()
        }
        web.loadUrl("file:///android_asset/web/index.html")
    }

    private fun deliverLink(i: Intent?) {
        val data = i?.data ?: return
        if (data.scheme != "mygo-music") return
        val js = "globalThis.__mygoLink = " + org.json.JSONObject.quote(data.toString()) +
            "; dispatchEvent(new Event('mygo-link'))"
        web.evaluateJavascript(js, null)
        intent = Intent(this, MainActivity::class.java) // consume once
    }

    override fun onNewIntent(i: Intent) {
        super.onNewIntent(i)
        setIntent(i)
        deliverLink(i)
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(req: Int, res: Int, data: Intent?) {
        super.onActivityResult(req, res, data)
        when (req) {
            REQ_FILE -> {
                val uris = mutableListOf<Uri>()
                data?.data?.let { uris.add(it) }
                data?.clipData?.let { for (k in 0 until it.itemCount) uris.add(it.getItemAt(k).uri) }
                fileChooser?.onReceiveValue(uris.toTypedArray())
                fileChooser = null
            }
            REQ_SAVE -> {
                val uri = data?.data
                val bytes = saveBytes
                saveBytes = null
                if (uri != null && bytes != null) {
                    Thread {
                        try {
                            contentResolver.openOutputStream(uri)?.use { it.write(bytes) }
                        } catch (e: Exception) {
                            runOnUiThread { Toast.makeText(this, "save failed: $e", Toast.LENGTH_SHORT).show() }
                        }
                    }.start()
                }
            }
        }
    }

    override fun onResume() {
        super.onResume()
        web.onResume()
    }

    override fun onDestroy() {
        WebLink.control = null
        web.destroy()
        super.onDestroy()
    }
}