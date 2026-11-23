package com.mygomusic.plus

import android.annotation.SuppressLint
import android.app.Activity
import android.content.Intent
import android.content.res.Configuration
import android.graphics.Color
import android.net.Uri
import android.os.Bundle
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Toast
import androidx.core.view.WindowCompat
import androidx.webkit.WebViewAssetLoader

// MainActivity hosts the MyGo interface and plugin runtime. NativePlayback
// connects it to the service that owns audio decoding and MediaSession.
class MainActivity : Activity() {

    private lateinit var web: WebView
    private lateinit var assetLoader: WebViewAssetLoader
    private lateinit var musicFolders: MusicFolders
    private var fileChooser: ValueCallback<Array<Uri>>? = null

    companion object {
        private const val REQ_FILE = 41
        private const val REQ_SAVE = 42
        private const val REQ_MUSIC_FOLDER = 43
        private var saveBytes: ByteArray? = null
        private var saveName: String? = null
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        web = PlayerWebRuntime.obtain(this)
        // Android 15 draws edge-to-edge; pad a wrapper (the WebView itself
        // ignores padding) so the app's top bar and player clear the system
        // bars.
        val root = android.widget.FrameLayout(this)
        root.addView(web, android.widget.FrameLayout.LayoutParams(
            android.widget.FrameLayout.LayoutParams.MATCH_PARENT,
            android.widget.FrameLayout.LayoutParams.MATCH_PARENT))
        setContentView(root)
        colorSystemBars(resources.configuration.isNightMode())
        androidx.core.view.ViewCompat.setOnApplyWindowInsetsListener(root) { v, insets ->
            val bars = insets.getInsets(androidx.core.view.WindowInsetsCompat.Type.systemBars())
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            insets
        }
        with(web.settings) {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            mediaPlaybackRequiresUserGesture = false
            allowFileAccess = true
            allowContentAccess = true
            mixedContentMode = android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW
        }
        web.addJavascriptInterface(Bridge(applicationContext) {
            android.os.Handler(android.os.Looper.getMainLooper()).post { PlayerWebRuntime.pickMusicFolder() }
        }, "AndroidBridge")
        // Serve assets over a virtual https domain: fetch() and OPFS need a
        // secure context, which file:///android_asset is not.
        musicFolders = MusicFolders(applicationContext.contentResolver)
        assetLoader = WebViewAssetLoader.Builder()
            .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(applicationContext))
            .addPathHandler("/native-music/", musicFolders)
            .addPathHandler("/native-github/", GitHubArchives())
            .addPathHandler("/native-gd/", GDWebRequests())
            .build()
        PlayerWebRuntime.useAssets(assetLoader)
        WebLink.device = { id, json ->
            val js = "globalThis.__mygoDeviceResult && __mygoDeviceResult($id, " +
                org.json.JSONObject.quote(json) + ")"
            PlayerWebRuntime.evaluate(js)
        }
        WebLink.install = { state ->
            val js = "globalThis.__mygoInstallState && __mygoInstallState(" +
                org.json.JSONObject.quote(state) + ")"
            PlayerWebRuntime.evaluate(js)
        }
        web.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(v: WebView, req: WebResourceRequest): WebResourceResponse? =
                assetLoader.shouldInterceptRequest(req.url)

            override fun shouldOverrideUrlLoading(v: WebView, req: WebResourceRequest): Boolean {
                val u = req.url.toString()
                if (u.startsWith("https://appassets.androidplatform.net/")) return false
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
        if (web.url == null) web.loadUrl("https://appassets.androidplatform.net/assets/web/index.html")
        else deliverLink(intent)
    }

    internal fun chooseMusicFolder() {
        val picker = Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).addFlags(
            Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION
        )
        try {
            startActivityForResult(picker, REQ_MUSIC_FOLDER)
        } catch (e: android.content.ActivityNotFoundException) {
            deliverMusicFolder(error = "The system folder picker is unavailable")
        }
    }

    private fun deliverMusicFolder(token: String = "", name: String = "", error: String = "") {
        if (isDestroyed) return
        val args = listOf(token, name, error).joinToString(",") { org.json.JSONObject.quote(it) }
        web.evaluateJavascript("globalThis.__mygoMusicFolderPicked?.($args)", null)
    }

    override fun onConfigurationChanged(configuration: Configuration) {
        super.onConfigurationChanged(configuration)
        val dark = configuration.isNightMode()
        colorSystemBars(dark)
        val theme = if (dark) "dark" else "light"
        web.evaluateJavascript("document.documentElement?.setAttribute('data-mygo-theme', '$theme')", null)
    }

    private fun colorSystemBars(dark: Boolean) {
        val background = if (dark) Color.rgb(20, 20, 22) else Color.WHITE
        window.decorView.setBackgroundColor(background)
        web.setBackgroundColor(background)
        WindowCompat.getInsetsController(window, window.decorView).apply {
            isAppearanceLightStatusBars = !dark
            isAppearanceLightNavigationBars = !dark
        }
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
            REQ_MUSIC_FOLDER -> {
                val uri = data?.data
                if (res != RESULT_OK || uri == null) {
                    deliverMusicFolder()
                } else {
                    Thread {
                        try {
                            val (token, name) = musicFolders.remember(uri)
                            runOnUiThread { deliverMusicFolder(token, name) }
                        } catch (e: Exception) {
                            runOnUiThread { deliverMusicFolder(error = "The selected folder couldn't be opened") }
                        }
                    }.start()
                }
            }
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

    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        // The app takes the button while it has somewhere to go back to;
        // before wasm boots, or at the app's root, the system has it instead.
        web.evaluateJavascript("globalThis.__mygoBack ? __mygoBack() : false") { handled ->
            if (handled != "true") super.onBackPressed()
        }
    }

    override fun onResume() {
        super.onResume()
        web.onResume()
        PlayerWebRuntime.visibility(this, true)
    }

    override fun onPause() {
        PlayerWebRuntime.visibility(this, false)
        super.onPause()
    }

    override fun onDestroy() {
        PlayerWebRuntime.detach(this)
        super.onDestroy()
    }
}
