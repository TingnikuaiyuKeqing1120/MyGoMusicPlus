package com.mygomusic.plus

import android.content.Context
import android.net.Uri
import android.util.Base64
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.security.MessageDigest
import java.util.UUID

// A content-addressed playback cache gives ExoPlayer ordinary files for audio
// kept in WebView's OPFS. Importing never removes the original download.
class NativeAudioFiles(context: Context) {
    private val root = File(context.cacheDir, "native-audio").apply { mkdirs() }
    private data class Import(val file: File, val target: File, val expected: Long,
                              val hash: String, val output: FileOutputStream,
                              val digest: MessageDigest, var written: Long = 0)
    private val imports = mutableMapOf<String, Import>()

    init {
        // A process exit can interrupt an import before JavaScript aborts it.
        root.listFiles()?.filter { it.extension == "part" }?.forEach { it.delete() }
    }

    @Synchronized
    fun close() {
        for (token in imports.keys.toList()) abort(token)
    }

    @Synchronized
    fun begin(hash: String, size: Long, type: String): String = answer {
        require(hash.matches(Regex("[0-9a-f]{64}")) && size >= 0) { "Invalid audio file" }
        val extension = type.takeIf { it.matches(Regex("[a-zA-Z0-9]{1,8}")) } ?: "audio"
        val target = File(root, "$hash.$extension")
        if (target.isFile && target.length() == size) {
            target.setLastModified(System.currentTimeMillis())
            return@answer JSONObject().put("uri", Uri.fromFile(target).toString())
        }
        val token = UUID.randomUUID().toString()
        val file = File(root, "$token.part")
        imports[token] = Import(file, target, size, hash, FileOutputStream(file), MessageDigest.getInstance("SHA-256"))
        JSONObject().put("token", token)
    }

    @Synchronized
    fun write(token: String, encoded: String): String = try {
        val job = imports[token] ?: error("Audio import expired")
        require(encoded.length <= 300_000) { "Audio block too large" }
        val bytes = Base64.decode(encoded, Base64.DEFAULT)
        require(job.written + bytes.size <= job.expected) { "Audio size changed during import" }
        job.output.write(bytes)
        job.digest.update(bytes)
        job.written += bytes.size
        ""
    } catch (e: Exception) { e.message ?: "Audio import failed" }

    @Synchronized
    fun commit(token: String): String = answer {
        val job = imports.remove(token) ?: error("Audio import expired")
        try {
            job.output.close()
            val actual = job.digest.digest().joinToString("") { "%02x".format(it) }
            require(job.written == job.expected && actual == job.hash) { "Audio checksum mismatch" }
            check(job.file.renameTo(job.target)) { "Could not finish audio import" }
            prune(job.target)
            JSONObject().put("uri", Uri.fromFile(job.target).toString())
        } finally {
            job.output.close()
            job.file.delete()
        }
    }

    @Synchronized
    fun abort(token: String) {
        imports.remove(token)?.let { it.output.close(); it.file.delete() }
    }

    private fun prune(keep: File) {
        val files = root.listFiles()?.filter { it.isFile && it.extension != "part" } ?: return
        var bytes = files.sumOf { it.length() }
        val used = NativePlayback.filesInUse()
        for (file in files.sortedBy { it.lastModified() }) {
            if (bytes <= 512L * 1024 * 1024) break
            if (file == keep || Uri.fromFile(file).toString() in used) continue
            if (System.currentTimeMillis() - file.lastModified() < 60_000) continue
            val size = file.length()
            if (file.delete()) bytes -= size
        }
    }

    private fun answer(action: () -> JSONObject): String = try {
        action().toString()
    } catch (e: Exception) {
        JSONObject().put("error", e.message ?: "Audio import failed").toString()
    }
}
