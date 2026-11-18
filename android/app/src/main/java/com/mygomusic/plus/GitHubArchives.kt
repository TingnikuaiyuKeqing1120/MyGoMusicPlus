package com.mygomusic.plus

import android.util.Base64
import android.webkit.WebResourceResponse
import androidx.webkit.WebViewAssetLoader
import java.io.ByteArrayInputStream
import java.io.FilterInputStream
import java.net.HttpURLConnection
import java.net.URL

// GitHub serves its release ZIPs on hosts that WebView can't fetch across
// origins. Only public archive requests and GitHub's own redirects go here.
class GitHubArchives : WebViewAssetLoader.PathHandler {
    companion object {
        private val releaseZip = Regex("/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/releases/download/[^/]+/[^/]+")
        private val apiZip = Regex("/repos/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/(zipball(?:/.*)?|releases/assets/[0-9]+)")
        private val redirects = setOf(301, 302, 303, 307, 308)
        private val archiveHosts = setOf("codeload.github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com")
        private val responseHeaders = mapOf("Cache-Control" to "no-store", "X-Content-Type-Options" to "nosniff")
    }

    override fun handle(path: String): WebResourceResponse {
        if (path.length > 8192) return error(400, "Invalid Archive URL")
        return try {
            val raw = String(Base64.decode(path, Base64.URL_SAFE or Base64.NO_WRAP), Charsets.UTF_8)
            download(URL(raw))
        } catch (e: Exception) {
            error(502, "GitHub Download Failed")
        }
    }

    private fun allowed(url: URL, redirected: Boolean): Boolean {
        if (url.protocol != "https" || url.userInfo != null || url.port !in listOf(-1, 443)) return false
        return when (url.host.lowercase()) {
            "github.com" -> releaseZip.matches(url.path)
            "api.github.com" -> apiZip.matches(url.path)
            in archiveHosts -> redirected
            else -> false
        }
    }

    private fun download(initial: URL): WebResourceResponse {
        var url = initial
        repeat(6) { hop ->
            if (!allowed(url, hop > 0)) return error(403, "Archive URL Forbidden")
            val conn = url.openConnection() as HttpURLConnection
            var streaming = false
            try {
                conn.instanceFollowRedirects = false
                conn.connectTimeout = 15000
                conn.readTimeout = 30000
                conn.setRequestProperty("User-Agent", "mygo-music")
                if (url.host == "api.github.com" && url.path.contains("/releases/assets/")) {
                    conn.setRequestProperty("Accept", "application/octet-stream")
                }
                val status = conn.responseCode
                if (status in redirects) {
                    val location = conn.getHeaderField("Location") ?: return error(502, "Missing GitHub Redirect")
                    url = URL(url, location)
                } else if (status == 200) {
                    val stream = object : FilterInputStream(conn.inputStream) {
                        override fun close() {
                            try { super.close() } finally { conn.disconnect() }
                        }
                    }
                    val result = WebResourceResponse("application/zip", null, 200, "OK", responseHeaders, stream)
                    streaming = true
                    return result
                } else {
                    return error(502, "GitHub Download Failed ($status)")
                }
            } finally {
                if (!streaming) conn.disconnect()
            }
        }
        return error(502, "Too Many GitHub Redirects")
    }

    private fun error(status: Int, reason: String) =
        WebResourceResponse("text/plain", "UTF-8", status, reason, responseHeaders, ByteArrayInputStream(byteArrayOf()))
}
