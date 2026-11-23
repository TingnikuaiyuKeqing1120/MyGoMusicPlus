package com.mygomusic.plus

import android.util.Base64
import android.webkit.WebResourceResponse
import androidx.webkit.WebViewAssetLoader
import java.io.ByteArrayInputStream
import java.io.FilterInputStream
import java.net.HttpURLConnection
import java.net.URL

// The two GD websites serve public JSON without browser CORS headers.
// Requests stay on this device and are restricted to the plugin's three GET paths.
class GDWebRequests : WebViewAssetLoader.PathHandler {
    override fun handle(path: String): WebResourceResponse {
        if (path.length > 16384) return error(400, "Invalid URL")
        return try {
            val url = URL(String(Base64.decode(path, Base64.URL_SAFE or Base64.NO_WRAP), Charsets.UTF_8))
            if (url.protocol != "https" || url.userInfo != null || url.port !in listOf(-1, 443) ||
                url.host !in setOf("music.gdstudio.org", "music.gdstudio.xyz") ||
                url.path !in setOf("/api.php", "/time", "/js/player.js")) {
                error(403, "URL Forbidden")
            } else {
                fetch(url)
            }
        } catch (e: Exception) {
            error(502, "GD Request Failed")
        }
    }

    private fun fetch(url: URL): WebResourceResponse {
        val conn = url.openConnection() as HttpURLConnection
        var streaming = false
        try {
            conn.instanceFollowRedirects = false
            conn.connectTimeout = 10000
            conn.readTimeout = 15000
            conn.useCaches = false
            val status = conn.responseCode
            if (status in 300..399) return error(502, "Unexpected Redirect")
            val input = if (status >= 400) conn.errorStream else conn.inputStream
            val stream = object : FilterInputStream(input ?: ByteArrayInputStream(byteArrayOf())) {
                override fun close() {
                    try { super.close() } finally { conn.disconnect() }
                }
            }
            val headers = mutableMapOf("Cache-Control" to "no-store", "X-Content-Type-Options" to "nosniff")
            conn.getHeaderField("Retry-After")?.let { headers["Retry-After"] = it }
            val response = WebResourceResponse(
                conn.contentType?.substringBefore(';') ?: "application/json", "UTF-8",
                status, conn.responseMessage?.takeIf { it.isNotBlank() } ?: "HTTP $status", headers, stream
            )
            streaming = true
            return response
        } finally {
            if (!streaming) conn.disconnect()
        }
    }

    private fun error(status: Int, reason: String) = WebResourceResponse(
        "text/plain", "UTF-8", status, reason, mapOf("Cache-Control" to "no-store"),
        ByteArrayInputStream(byteArrayOf())
    )
}
