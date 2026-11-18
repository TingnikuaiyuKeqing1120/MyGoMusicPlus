package com.mygomusic.plus

import android.content.ContentResolver
import android.content.Intent
import android.net.Uri
import android.provider.DocumentsContract
import android.provider.DocumentsContract.Document
import android.util.Base64
import android.webkit.WebResourceResponse
import androidx.webkit.WebViewAssetLoader
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.FileNotFoundException

// Serves only documents in a folder the listener chose in the system picker.
// The app reads ranges on demand instead of copying the library into WebView storage.
class MusicFolders(private val resolver: ContentResolver) : WebViewAssetLoader.PathHandler {
    companion object {
        private const val BASE64_FLAGS = Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING
        private const val MAX_READ = 256 * 1024
        private val columns = arrayOf(
            Document.COLUMN_DOCUMENT_ID, Document.COLUMN_DISPLAY_NAME, Document.COLUMN_MIME_TYPE,
            Document.COLUMN_SIZE, Document.COLUMN_LAST_MODIFIED
        )
    }

    fun remember(tree: Uri): Pair<String, String> {
        val id = DocumentsContract.getTreeDocumentId(tree)
        val root = DocumentsContract.buildDocumentUriUsingTree(tree, id)
        val name = resolver.query(root, arrayOf(Document.COLUMN_DISPLAY_NAME), null, null, null)?.use {
            if (it.moveToFirst()) it.getString(0) else null
        } ?: throw FileNotFoundException()
        resolver.takePersistableUriPermission(tree, Intent.FLAG_GRANT_READ_URI_PERMISSION)
        return encode(tree.toString()) to name
    }

    override fun handle(path: String): WebResourceResponse {
        val parts = path.split('/')
        if (parts.size < 3) return failure(404, "Not Found")
        return try {
            val tree = Uri.parse(decode(parts[0]))
            if (resolver.persistedUriPermissions.none { it.isReadPermission && it.uri == tree }) {
                return failure(403, "Forbidden")
            }
            val id = if (parts[2] == "-") DocumentsContract.getTreeDocumentId(tree) else decode(parts[2])
            when (parts[1]) {
                "list" -> list(tree, id)
                "read" -> {
                    val offset = parts.getOrNull(3)?.toLongOrNull()
                    val length = parts.getOrNull(4)?.toIntOrNull()
                    if (offset == null || offset < 0 || length == null || length !in 1..MAX_READ) {
                        return failure(400, "Invalid Range")
                    }
                    read(tree, id, offset, length)
                }
                else -> failure(404, "Not Found")
            }
        } catch (e: SecurityException) {
            failure(403, "Forbidden")
        } catch (e: FileNotFoundException) {
            failure(404, "Not Found")
        } catch (e: Exception) {
            failure(500, "Read Failed")
        }
    }

    private fun list(tree: Uri, id: String): WebResourceResponse {
        val children = DocumentsContract.buildChildDocumentsUriUsingTree(tree, id)
        val entries = JSONArray()
        val cursor = resolver.query(children, columns, null, null, null) ?: throw FileNotFoundException()
        cursor.use {
            while (it.moveToNext()) {
                entries.put(JSONObject().apply {
                    put("id", encode(it.getString(0)))
                    put("name", it.getString(1))
                    put("dir", it.getString(2) == Document.MIME_TYPE_DIR)
                    put("size", it.getLong(3))
                    put("mtime", it.getLong(4))
                })
            }
        }
        return WebResourceResponse("application/json", "UTF-8",
            ByteArrayInputStream(entries.toString().toByteArray(Charsets.UTF_8)))
    }

    private fun read(tree: Uri, id: String, offset: Long, length: Int): WebResourceResponse {
        val uri = DocumentsContract.buildDocumentUriUsingTree(tree, id)
        val input = resolver.openInputStream(uri) ?: throw FileNotFoundException()
        val bytes = ByteArray(length)
        val count = input.use {
            var remaining = offset
            while (remaining > 0L) {
                val skipped = it.skip(remaining)
                if (skipped > 0L) remaining -= skipped
                else if (it.read() == -1) return@use 0
                else remaining--
            }
            var n = 0
            while (n < length) {
                val got = it.read(bytes, n, length - n)
                if (got <= 0) break
                n += got
            }
            n
        }
        return WebResourceResponse("application/octet-stream", null, ByteArrayInputStream(bytes, 0, count))
    }

    private fun failure(code: Int, reason: String) =
        WebResourceResponse("text/plain", "UTF-8", code, reason, emptyMap(), ByteArrayInputStream(byteArrayOf()))

    private fun encode(s: String): String = Base64.encodeToString(s.toByteArray(Charsets.UTF_8), BASE64_FLAGS)
    private fun decode(s: String): String = String(Base64.decode(s, BASE64_FLAGS), Charsets.UTF_8)
}
