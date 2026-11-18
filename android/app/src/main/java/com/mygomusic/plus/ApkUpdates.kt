package com.mygomusic.plus

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.os.Build
import androidx.core.content.FileProvider
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.nio.file.Files
import java.nio.file.StandardCopyOption.ATOMIC_MOVE
import java.nio.file.StandardCopyOption.REPLACE_EXISTING
import java.security.MessageDigest

// Completed downloads live in app files so the installer can be reopened
// after cancellation, Activity recreation, or a cold start.
internal object ApkUpdates {
    private const val RELEASE_PATH = "/TingnikuaiyuKeqing1120/MyGoMusicPlus/releases/download/"
    private const val MIRROR = "https://gh-proxy.org/"
    private val sha256 = Regex("sha256:[0-9a-fA-F]{64}")
    private val lock = Any()
    private var busy = false
    private var state = ""
    private var ready = ""

    private data class SavedApk(val file: File, val tag: String)

    fun restore(ctx: Context): String = synchronized(lock) {
        if (!busy) pending(ctx).delete()
        ready = try {
            savedApk(ctx)?.tag.orEmpty()
        } catch (e: Exception) {
            if (!busy) state = "failed: " + (e.message ?: e.javaClass.simpleName)
            ""
        }
        snapshot()
    }

    fun download(ctx: Context, tag: String, url: String, digest: String, size: Long, mirror: Boolean) {
        runJob("downloading") {
            val cached = synchronized(lock) { savedApk(ctx) }
            val wanted = releaseTag(tag)
            val apk = if (cached?.tag == wanted) cached else {
                val staging = pending(ctx)
                try {
                    staging.parentFile?.mkdirs()
                    fetch(url, digest, size, mirror, staging)
                    val checked = readApk(ctx, staging, installedVersion(ctx))
                    check(checked?.tag == wanted) { "The downloaded APK does not match release $wanted" }
                    val complete = complete(ctx)
                    synchronized(lock) {
                        Files.move(staging.toPath(), complete.toPath(), ATOMIC_MOVE, REPLACE_EXISTING)
                        legacy(ctx).delete()
                    }
                    SavedApk(complete, wanted)
                } finally {
                    staging.delete()
                }
            }
            report("", apk.tag)
            launchInstaller(ctx, apk.file)
        }
    }

    fun reopen(ctx: Context) {
        runJob("") {
            val apk = synchronized(lock) { savedApk(ctx) }
            report("", apk?.tag.orEmpty())
            checkNotNull(apk) { "The saved APK is missing. Check for updates to download it again." }
            launchInstaller(ctx, apk.file)
        }
    }

    private fun runJob(initial: String, action: () -> Unit) {
        synchronized(lock) {
            if (busy) return
            busy = true
        }
        report(initial)
        Thread({
            try {
                action()
            } catch (e: Exception) {
                report("failed: " + (e.message ?: e.javaClass.simpleName))
            } finally {
                synchronized(lock) { busy = false }
            }
        }, "mygo-apk-update").start()
    }

    private fun snapshot(): String = JSONObject().put("state", state).put("ready", ready).toString()

    private fun report(next: String, savedTag: String? = null) {
        val json = synchronized(lock) {
            state = next
            if (savedTag != null) ready = savedTag
            snapshot()
        }
        WebLink.install?.invoke(json)
    }

    private fun complete(ctx: Context) = File(ctx.filesDir, "updates/update.apk")
    private fun pending(ctx: Context) = File(ctx.filesDir, "updates/pending.apk")
    private fun legacy(ctx: Context) = File(ctx.cacheDir, "update.apk")

    private fun savedApk(ctx: Context): SavedApk? {
        val saved = complete(ctx)
        val old = legacy(ctx)
        if (!saved.isFile && !old.isFile) return null
        val installed = installedVersion(ctx)
        readApk(ctx, saved, installed)?.let { return it }
        saved.delete()
        val fromCache = readApk(ctx, old, installed)
        if (fromCache == null) {
            old.delete()
            return null
        }
        // Older builds used cacheDir/update.apk. Recover a complete upgrade
        // from there when Android has not evicted it.
        saved.parentFile?.mkdirs()
        Files.move(old.toPath(), saved.toPath(), ATOMIC_MOVE, REPLACE_EXISTING)
        return fromCache.copy(file = saved)
    }

    @Suppress("DEPRECATION")
    private fun installedVersion(ctx: Context) = versionCode(ctx.packageManager.getPackageInfo(ctx.packageName, 0))

    @Suppress("DEPRECATION")
    private fun readApk(ctx: Context, file: File, installed: Long): SavedApk? {
        if (!file.isFile) return null
        val info = ctx.packageManager.getPackageArchiveInfo(file.path, 0) ?: return null
        val version = info.versionName ?: return null
        if (version.isBlank() || info.packageName != ctx.packageName || versionCode(info) <= installed) return null
        return SavedApk(file, releaseTag(version))
    }

    @Suppress("DEPRECATION")
    private fun versionCode(info: PackageInfo): Long =
        if (Build.VERSION.SDK_INT >= 28) info.longVersionCode else info.versionCode.toLong()

    private fun releaseTag(version: String) = "v" + version.removePrefix("v")

    private fun fetch(url: String, digest: String, size: Long, mirror: Boolean, to: File) {
        val official = URL(url)
        require(official.protocol == "https" && official.host.equals("github.com", ignoreCase = true) &&
            official.userInfo == null && official.port in listOf(-1, 443) &&
            official.query == null && official.ref == null && official.path.startsWith(RELEASE_PATH) &&
            official.path.endsWith(".apk", ignoreCase = true)) { "Unexpected APK release URL" }
        require(size > 0) { "GitHub did not publish the APK size" }
        require((digest.isEmpty() && !mirror) || sha256.matches(digest)) { "A valid GitHub APK checksum is required" }
        val endpoint = if (mirror) URL(MIRROR + official.toExternalForm()) else official
        val conn = endpoint.openConnection() as HttpURLConnection
        try {
            conn.connectTimeout = 30000
            conn.readTimeout = 60000
            conn.setRequestProperty("User-Agent", "MyGoMusicPlus")
            conn.setRequestProperty("Accept-Encoding", "identity")
            val code = conn.responseCode
            check(code == HttpURLConnection.HTTP_OK && conn.url.protocol == "https") { "APK download returned HTTP $code" }
            val hash = MessageDigest.getInstance("SHA-256")
            var done = 0L
            var lastPercent = -1
            conn.inputStream.use { input ->
                to.outputStream().use { out ->
                    val buffer = ByteArray(64 * 1024)
                    while (true) {
                        val count = input.read(buffer)
                        if (count < 0) break
                        done += count
                        check(done <= size) { "The download is larger than the GitHub APK" }
                        out.write(buffer, 0, count)
                        hash.update(buffer, 0, count)
                        val percent = (done * 100.0 / size).toInt()
                        if (percent != lastPercent) {
                            lastPercent = percent
                            report("downloading:$percent")
                        }
                    }
                    out.fd.sync()
                }
            }
            check(done == size) { "The APK download is incomplete" }
            val actual = "sha256:" + hash.digest().joinToString("") { "%02x".format(it.toInt() and 0xff) }
            check(digest.isEmpty() || actual.equals(digest, ignoreCase = true)) { "The APK checksum does not match GitHub" }
        } finally {
            conn.disconnect()
        }
    }

    private fun launchInstaller(ctx: Context, file: File) {
        if (ctx is Activity && (ctx.isFinishing || ctx.isDestroyed)) return
        val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".fileprovider", file)
        val intent = Intent(Intent.ACTION_VIEW).setDataAndType(uri, "application/vnd.android.package-archive")
            .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        ctx.startActivity(intent)
    }
}
