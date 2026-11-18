package com.mygomusic.plus

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.content.res.Configuration
import android.net.Uri
import android.webkit.JavascriptInterface

internal fun Configuration.isNightMode(): Boolean =
    (uiMode and Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES

// Bridge is the JS->native surface the web media-bridge calls. It forwards
// playback metadata/state/position to PlaybackService (notification, lock
// screen, audio focus) and offers share / open-external.
class Bridge(private val ctx: Context, private val chooseMusicFolder: () -> Unit) {

    @JavascriptInterface
    fun pickMusicFolder() = chooseMusicFolder()

    @JavascriptInterface
    fun systemDark(): Boolean = ctx.resources.configuration.isNightMode()

    @JavascriptInterface
    fun hasGitHubArchiveLoader(): Boolean = true

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
    fun openGitHubDevicePage(preferApp: Boolean): Boolean {
        val page = Uri.parse("https://github.com/login/device")
        val view = Intent(Intent.ACTION_VIEW, page)
            .addCategory(Intent.CATEGORY_BROWSABLE)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (preferApp && tryOpen(Intent(view).setPackage("com.github.android"))) return true

        // Resolve through browser apps so the fallback also works if GitHub
        // Mobile accepts the link but doesn't show the device form.
        view.selector = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_APP_BROWSER)
        return tryOpen(view)
    }

    private fun tryOpen(intent: Intent): Boolean = try {
        ctx.startActivity(intent)
        true
    } catch (_: ActivityNotFoundException) {
        false
    } catch (_: SecurityException) {
        false
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

    // GitHub's device flow: the sign-in endpoints aren't open to a browser's
    // fetch, so the shell makes the requests. Each returns at once and the
    // answer goes back through WebLink.device, by the id it came in with.
    @JavascriptInterface
    fun githubDeviceStart(clientId: String, scope: String, id: Int) {
        devicePost(id, "https://github.com/login/device/code",
            "client_id=" + enc(clientId) + "&scope=" + enc(scope))
    }

    @JavascriptInterface
    fun githubDevicePoll(clientId: String, deviceCode: String, id: Int) {
        devicePost(id, "https://github.com/login/oauth/access_token",
            "client_id=" + enc(clientId) + "&device_code=" + enc(deviceCode) +
                "&grant_type=" + enc("urn:ietf:params:oauth:grant-type:device_code"))
    }

    private fun enc(s: String) = java.net.URLEncoder.encode(s, "UTF-8")

    private fun devicePost(id: Int, url: String, body: String) {
        Thread {
            val res = try {
                val conn = java.net.URL(url).openConnection() as java.net.HttpURLConnection
                conn.requestMethod = "POST"
                conn.doOutput = true
                conn.connectTimeout = 15000
                conn.readTimeout = 15000
                conn.setRequestProperty("Accept", "application/json")
                conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
                val stream = if (conn.responseCode >= 400) conn.errorStream else conn.inputStream
                stream?.use { it.readBytes().toString(Charsets.UTF_8) }
                    ?: "{\"error\":\"http_${conn.responseCode}\"}"
            } catch (e: Exception) {
                "{\"error\":\"network\",\"error_description\":" +
                    org.json.JSONObject.quote(e.message ?: "connection failed") + "}"
            }
            WebLink.device?.invoke(id, res)
        }.start()
    }

    @JavascriptInterface
    fun downloadUpdateApk(tag: String, url: String, digest: String, size: Long, mirror: Boolean) =
        ApkUpdates.download(ctx, tag, url, digest, size, mirror)

    @JavascriptInterface
    fun openUpdateInstaller() = ApkUpdates.reopen(ctx)

    @JavascriptInterface
    fun updateApkState(): String = ApkUpdates.restore(ctx)
}
