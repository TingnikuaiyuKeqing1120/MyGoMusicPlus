package com.mygomusic.plus

import android.content.Context
import androidx.media3.common.C
import androidx.media3.database.StandaloneDatabaseProvider
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.cache.Cache
import androidx.media3.datasource.cache.CacheDataSource
import androidx.media3.datasource.cache.CacheEvictor
import androidx.media3.datasource.cache.CacheSpan
import androidx.media3.datasource.cache.ContentMetadata
import androidx.media3.datasource.cache.SimpleCache
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.TreeSet
import java.util.concurrent.atomic.AtomicLong

// Native streaming uses the same cache limit and stable song keys as Go.
// Complete spans can play offline without asking an online source for a URL.
class NativeCache private constructor(context: Context) {
    companion object {
        @Volatile private var instance: NativeCache? = null
        @Synchronized fun get(context: Context): NativeCache = instance
            ?: NativeCache(context.applicationContext).also { instance = it }
    }
    private val metadataFile = File(context.filesDir,"native-streams.json")
    private val metadata = runCatching { JSONObject(metadataFile.readText()) }.getOrElse { JSONObject() }
    val revision = AtomicLong(0)
    @Volatile private var limit = 512L*1024*1024
    private val evictor = object : CacheEvictor {
        var limit = 512L*1024*1024
        val spans = TreeSet(compareBy<CacheSpan> { it.lastTouchTimestamp }.thenBy { it.key }.thenBy { it.position })
        override fun requiresCacheSpanTouches() = true
        override fun onCacheInitialized() {}
        override fun onStartFile(cache: Cache,key: String,position: Long,length: Long) {
            prune(cache, if (length == C.LENGTH_UNSET.toLong()) 0 else length)
        }
        override fun onSpanAdded(cache: Cache,span: CacheSpan) { spans.add(span); prune(cache,0); revision.incrementAndGet() }
        override fun onSpanRemoved(cache: Cache,span: CacheSpan) { spans.remove(span); revision.incrementAndGet() }
        override fun onSpanTouched(cache: Cache,old: CacheSpan,new: CacheSpan) { spans.remove(old); spans.add(new) }
        fun prune(cache: Cache,extra: Long) {
            while (cache.cacheSpace+extra > limit && spans.isNotEmpty()) cache.removeSpan(spans.first())
        }
    }
    private val cache = SimpleCache(File(context.cacheDir,"native-streams"),evictor,StandaloneDatabaseProvider(context))

    @Synchronized
    fun store(key: String,stream: JSONObject) {
        if (limit <= 0 || !stream.optString("url").startsWith("http")) return
        metadata.put(key,JSONObject(stream.toString()).put("resolvedAt",System.currentTimeMillis()))
        // Retain a bounded number of prepared entries that have not cached bytes yet.
        val unused = metadata.keys().asSequence().filter { cache.getCachedSpans(it).isEmpty() }
            .sortedBy { metadata.getJSONObject(it).optLong("resolvedAt") }.toList()
        for (old in unused.take(maxOf(0,unused.size-100))) metadata.remove(old)
        val temporary = File(metadataFile.parentFile,metadataFile.name+".part")
        temporary.writeText(metadata.toString())
        check(temporary.renameTo(metadataFile)) { "Could not save audio cache index" }
        revision.incrementAndGet()
    }

    @Synchronized
    fun offline(key: String): JSONObject? {
        val length = ContentMetadata.getContentLength(cache.getContentMetadata(key))
        if (length <= 0 || !cache.isCached(key,0,length)) return null
        return metadata.optJSONObject(key)?.let { JSONObject(it.toString()) }
    }

    fun find(key: String): NativeStream? {
        val qualities = listOf("standard", "high", "lossless")
        val quality = key.substringAfterLast('-')
        val first = qualities.indexOf(quality).coerceAtLeast(0)
        for (candidate in qualities.drop(first)) {
            val storedKey = key.substringBeforeLast('-') + "-" + candidate
            offline(storedKey)?.let { return NativeStream(it, storedKey) }
        }
        return null
    }

    fun source(upstream: DataSource.Factory): DataSource {
        if (limit <= 0) return upstream.createDataSource()
        return CacheDataSource.Factory().setCache(cache).setUpstreamDataSourceFactory(upstream)
            .setFlags(CacheDataSource.FLAG_IGNORE_CACHE_ON_ERROR).createDataSource()
    }

    fun configure(limit: Long) {
        this.limit = limit.coerceAtLeast(0)
        synchronized(cache) {
            evictor.limit = limit.coerceAtLeast(0)
            evictor.prune(cache,0)
        }
    }

    fun clear() {
        for (key in cache.keys.toList()) cache.removeResource(key)
        revision.incrementAndGet()
    }

    @Synchronized
    fun status(): JSONObject {
        val complete = metadata.keys().asSequence().filter { offline(it) != null }.toList()
        return JSONObject().put("bytes",cache.cacheSpace).put("keys",JSONArray(complete))
    }
}
