package com.mygomusic.plus

import android.content.Context
import android.net.Uri
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import androidx.media3.datasource.DefaultDataSource
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.datasource.TransferListener
import org.json.JSONObject
import java.io.IOException
import java.io.File
import java.io.InterruptedIOException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import okhttp3.OkHttpClient
import androidx.media3.datasource.HttpDataSource.InvalidResponseCodeException

class NativeStream(val data: JSONObject, val cacheKey: String) {
    val url: String = data.getString("url")
    val gain = data.optDouble("gain",0.0)
    val peak = data.optDouble("peak",0.0)
    val headers: Map<String,String> = data.optJSONObject("headers")?.let { obj ->
        obj.keys().asSequence().associateWith { obj.getString(it) }
    } ?: emptyMap()
}

// URL lookup happens through the source plugin. ExoPlayer's loading thread
// waits for it, while decoding/output and the Android UI keep running.
class NativeSources(private val cache: NativeCache) {
    companion object {
        private val httpClient = OkHttpClient.Builder()
            .connectTimeout(15, TimeUnit.SECONDS).readTimeout(30, TimeUnit.SECONDS).build()
    }
    private class Pending(val cacheKey: String) {
        val done = CountDownLatch(1)
        @Volatile var stream: NativeStream? = null
        @Volatile var error: String? = null
    }
    private val pending = ConcurrentHashMap<String, Pending>()
    @Volatile private var generation = -1
    @Volatile var keys: Map<Int,String> = emptyMap()

    @Synchronized
    fun request(generation: Int, slot: Int) {
        if (generation != this.generation) return
        val key = "$generation/$slot"
        // A previously visited OPFS copy may have been evicted while other
        // tracks played. Ask the bridge to materialize it again when revisited.
        val previous = pending[key]?.stream?.url
        if (previous?.startsWith("file:") == true && !File(Uri.parse(previous).path!!).isFile) {
            pending.remove(key)
        }
        val entry = Pending(keys[slot] ?: return)
        if (pending.putIfAbsent(key, entry) == null) {
            val stored = cache.find(entry.cacheKey)
            if (stored != null) {
                entry.stream = stored
                entry.done.countDown()
            } else PlayerWebRuntime.evaluate("globalThis.__mygoNativeResolve?.($generation,$slot)")
        }
    }

    fun complete(result: JSONObject) {
        val key = "${result.getInt("generation")}/${result.getInt("slot")}"
        val entry = pending[key] ?: return
        val info = result.optJSONObject("stream")
        if (info == null) entry.error = result.optString("error", "The source returned no audio")
        else {
            entry.stream = NativeStream(info,entry.cacheKey)
            runCatching { cache.store(entry.cacheKey,info) }
        }
        entry.done.countDown()
    }

    fun get(generation: Int, slot: Int): NativeStream? = pending["$generation/$slot"]?.stream

    @Synchronized
    fun retry(generation: Int, slot: Int) {
        pending.remove("$generation/$slot")
        request(generation,slot)
    }

    fun await(generation: Int, slot: Int): NativeStream {
        request(generation, slot)
        val entry = pending["$generation/$slot"] ?: throw IOException("Playback was replaced")
        try {
            if (!entry.done.await(120, TimeUnit.SECONDS)) throw IOException("The music source timed out")
        } catch (e: InterruptedException) {
            Thread.currentThread().interrupt()
            throw InterruptedIOException("Playback was cancelled")
        }
        return entry.stream ?: throw IOException(entry.error ?: "The music source failed")
    }

    @Synchronized
    fun reset(generation: Int = -1) {
        this.generation = generation
        val entries = pending.values.toList()
        pending.clear()
        for (entry in entries) { entry.error = "Playback was replaced"; entry.done.countDown() }
    }

    fun files(slots: Set<Int>): Set<String> = slots.mapNotNull { get(generation,it)?.url?.takeIf { url -> url.startsWith("file:") } }.toSet()

    fun factory(context: Context): DataSource.Factory = DataSource.Factory {
        object : DataSource {
            private var source: DataSource? = null
            private val listeners = mutableListOf<TransferListener>()
            override fun addTransferListener(listener: TransferListener) { listeners.add(listener) }
            override fun open(spec: DataSpec): Long {
                val parts = spec.uri.pathSegments
                val generation = spec.uri.host!!.toInt()
                val slot = parts.single().toInt()
                val stream = await(generation, slot)
                return try {
                    openResolved(spec,stream)
                } catch (e: InvalidResponseCodeException) {
                    if (e.responseCode !in listOf(401,403,404)) throw e
                    close()
                    retry(generation,slot)
                    openResolved(spec,await(generation,slot))
                }
            }
            private fun openResolved(spec: DataSpec, stream: NativeStream): Long {
                val origin = Uri.parse(stream.url).host.orEmpty().lowercase()
                val client = httpClient.newBuilder().addNetworkInterceptor { chain ->
                    val request = chain.request()
                    val host = request.url.host.lowercase()
                    val trusted = host == origin || host.endsWith(".$origin")
                    val filtered = request.newBuilder()
                    for ((name,value) in stream.headers) {
                        if (trusted) filtered.header(name,value) else filtered.removeHeader(name)
                    }
                    chain.proceed(filtered.build())
                }.build()
                val http = OkHttpDataSource.Factory(client).setUserAgent("MyGo Music Plus")
                val factory = DefaultDataSource.Factory(context,http)
                val opened = if (stream.url.startsWith("http")) cache.source(factory) else factory.createDataSource()
                source = opened
                listeners.forEach(opened::addTransferListener)
                return opened.open(spec.buildUpon().setUri(Uri.parse(stream.url)).setKey(stream.cacheKey).build())
            }
            override fun read(buffer: ByteArray, offset: Int, length: Int): Int = source!!.read(buffer, offset, length)
            override fun getUri(): Uri? = source?.uri
            override fun getResponseHeaders(): Map<String, List<String>> = source?.responseHeaders ?: emptyMap()
            override fun close() { source?.close(); source = null }
        }
    }
}
