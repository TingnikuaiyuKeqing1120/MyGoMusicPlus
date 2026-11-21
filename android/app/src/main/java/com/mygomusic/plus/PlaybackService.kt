package com.mygomusic.plus

import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.audio.AudioSink
import androidx.media3.exoplayer.audio.DefaultAudioSink
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.exoplayer.analytics.AnalyticsListener
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import org.json.JSONArray
import org.json.JSONObject
import kotlin.math.pow
import kotlin.math.cos
import kotlin.math.PI

// Media3 owns playback and advances the resolved queue while the WebView is
// busy or the activity is detached. Go supplies plugin URLs and receives state.
class PlaybackService : MediaSessionService() {
    lateinit var sources: NativeSources
        private set
    private lateinit var cache: NativeCache
    private var cacheRevision = -1L
    private lateinit var player: ExoPlayer
    private lateinit var session: MediaSession
    private val main = Handler(Looper.getMainLooper())
    private val processor = NativeAudioProcessor()
    private var generation = -1
    private var serial = 0L
    private var volume = 0.64
    private var normalize = false
    private var error = ""
    private var applying = false
    private var released = false
    private var fadeSerial = 0
    private var pausing = false
    private var underruns = 0
    @Volatile var snapshot = "{}"
        private set
    @Volatile var filesInUse: Set<String> = emptySet()
        private set
    private val ticker = object : Runnable {
        override fun run() {
            if (released) return
            processor.measureSpectrum = PlayerWebRuntime.foreground
            publish(PlayerWebRuntime.foreground)
            main.postDelayed(this, 250)
        }
    }

    override fun onCreate() {
        super.onCreate()
        cache = NativeCache.get(this)
        sources = NativeSources(cache)
        val renderers = object : DefaultRenderersFactory(this) {
            override fun buildAudioSink(context: Context, enableFloatOutput: Boolean,
                                        enableAudioOutputPlaybackParameters: Boolean): AudioSink {
                return DefaultAudioSink.Builder(context)
                    .setEnableFloatOutput(false)
                    .setAudioProcessors(arrayOf(processor))
                    .build()
            }
        }
        player = ExoPlayer.Builder(this, renderers)
            .setMediaSourceFactory(DefaultMediaSourceFactory(sources.factory(this)))
            .build().apply {
                setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA)
                    .setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), true)
                setHandleAudioBecomingNoisy(true)
                setWakeMode(C.WAKE_MODE_NETWORK)
            }
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        session = MediaSession.Builder(this, player).setSessionActivity(open).build()
        // The in-process bridge starts us directly, without a MediaController
        // binding that would register the session through onGetSession.
        addSession(session)
        player.addAnalyticsListener(object : AnalyticsListener {
            override fun onAudioUnderrun(eventTime: AnalyticsListener.EventTime, bufferSize: Int,
                                         bufferSizeMs: Long, elapsedSinceLastFeedMs: Long) { underruns++ }
        })
        player.addListener(object : Player.Listener {
            override fun onPlaybackStateChanged(playbackState: Int) {
                // MediaSession prepares an errored player before calling play,
                // clearing playerError before onPlayWhenReadyChanged arrives.
                if (playbackState == Player.STATE_BUFFERING && error.isNotEmpty()) {
                    error = ""
                    player.currentMediaItem?.mediaId?.toIntOrNull()?.let { sources.retry(generation, it) }
                    publish()
                }
            }

            override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
                // MediaSession and audio focus can resume a player faded out
                // by the in-app pause button. Restore its audible volume too.
                if (playWhenReady) {
                    pausing = false
                    fade(1f, 260)
                } else {
                    fadeSerial++
                    pausing = false
                    player.volume = 1f
                }
            }

            override fun onEvents(player: Player, events: Player.Events) {
                if (applying) return
                if (events.size() == 1 && events.contains(Player.EVENT_VOLUME_CHANGED)) return
                prefetch()
                applyGain()
                publish()
            }
            override fun onPlayerError(failure: PlaybackException) {
                error = failure.cause?.message ?: failure.message ?: failure.errorCodeName
                publish()
            }
        })
        NativePlayback.attached(this)
        main.post(ticker)
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession = session

    fun command(command: JSONObject) {
        val action = command.getString("action")
        val incoming = command.getInt("generation")
        if (action != "queue" && incoming != generation) return
        if (incoming < generation) return
        serial = command.optLong("serial")
        when (action) {
            "queue" -> queue(command)
            "play" -> {
                if (command.optBoolean("play")) {
                    pausing = false
                    if (player.playbackState == Player.STATE_ENDED) player.seekTo(0)
                    player.play()
                    applyGain()
                    fade(1f,260)
                } else {
                    pausing = true
                    fade(0f,180) { player.pause(); pausing = false; publish() }
                }
            }
            "seek" -> { cancelFade(); player.seekTo(command.optLong("position").coerceAtLeast(0)) }
            "next", "previous" -> {
                cancelFade()
                if (player.mediaItemCount > 0) {
                    val step = if (action == "next") 1 else -1
                    val index = (player.currentMediaItemIndex + step + player.mediaItemCount) % player.mediaItemCount
                    player.seekTo(index, 0)
                    if (error.isNotEmpty()) { error = ""; player.prepare() }
                    player.play()
                }
            }
            "options" -> options(command)
            "stop" -> { cancelFade(); player.stop(); player.clearMediaItems(); sources.reset(); filesInUse = emptySet() }
        }
        publish(forceCache = true)
    }

    private fun queue(command: JSONObject) {
        applying = true
        try {
            val incoming = command.getInt("generation")
            val newQueue = incoming != generation
            error = ""
            if (newQueue) { fadeSerial++; pausing = false; player.volume = 1f }
            if (newQueue) sources.reset(incoming)
            generation = incoming
            val items = command.getJSONArray("items")
            sources.keys = (0 until items.length()).associate { index ->
                val entry = items.getJSONObject(index)
                entry.getInt("slot") to entry.getString("cacheKey")
            }
            val media = (0 until items.length()).map { item(items.getJSONObject(it)) }
            val requested = command.getInt("slot").toString()
            if (newQueue) {
                val at = media.indexOfFirst { it.mediaId == requested }.coerceAtLeast(0)
                player.setMediaItems(media, at, command.optLong("position"))
                player.prepare()
                player.playWhenReady = command.optBoolean("play")
            } else {
                // Move existing items so shuffle/append keeps the current stream.
                for ((index, target) in media.withIndex()) {
                    val found = (index until player.mediaItemCount).firstOrNull { player.getMediaItemAt(it).mediaId == target.mediaId }
                    if (found == null) player.addMediaItem(index, target)
                    else if (found != index) player.moveMediaItem(found, index)
                }
                if (player.mediaItemCount > media.size) player.removeMediaItems(media.size, player.mediaItemCount)
                if (player.playbackState == Player.STATE_ENDED && command.optBoolean("play") && player.hasNextMediaItem()) {
                    player.seekToNextMediaItem()
                    player.play()
                }
            }
            options(command)
        } finally { applying = false }
        prefetch()
    }

    private fun item(item: JSONObject): MediaItem {
        val track = item.getJSONObject("track")
        val artists = track.optJSONArray("artists") ?: JSONArray()
        val artist = (0 until artists.length()).joinToString(", ") { artists.getJSONObject(it).optString("name") }
        val album = track.optJSONObject("album")
        val metadata = MediaMetadata.Builder().setTitle(track.optString("name"))
            .setArtist(artist).setAlbumTitle(album?.optString("name"))
        album?.optString("imageUrl")?.takeIf { it.isNotEmpty() }?.let { metadata.setArtworkUri(Uri.parse(it)) }
        val slot = item.getInt("slot")
        return MediaItem.Builder().setMediaId(slot.toString())
            .setUri("mygo-native://$generation/$slot")
            .setMediaMetadata(metadata.build()).build()
    }

    private fun options(command: JSONObject) {
        volume = command.optDouble("volume", volume).coerceIn(0.0, 1.0)
        normalize = command.optBoolean("normalize", normalize)
        player.repeatMode = when (command.optInt("repeat")) {
            1 -> Player.REPEAT_MODE_ALL
            2 -> Player.REPEAT_MODE_ONE
            else -> Player.REPEAT_MODE_OFF
        }
        applyGain()
    }

    private fun applyGain() {
        val slot = player.currentMediaItem?.mediaId?.toIntOrNull() ?: return
        val stream = sources.get(generation, slot)
        var gain = 1.0
        if (normalize && stream != null && (stream.gain != 0.0 || stream.peak != 0.0)) {
            gain = 10.0.pow((stream.gain + 4) / 20)
            if (stream.peak > 0) gain = minOf(gain, 1 / stream.peak)
        }
        processor.gain = volume * gain
    }

    private fun fade(target: Float, duration: Long, done: () -> Unit = {}) {
        val version = ++fadeSerial
        val from = player.volume
        val started = SystemClock.elapsedRealtime()
        val tick = object : Runnable {
            override fun run() {
                if (released || version != fadeSerial) return
                val fraction = ((SystemClock.elapsedRealtime()-started).toDouble()/duration).coerceAtMost(1.0)
                player.volume = (from+(target-from)*(0.5-0.5*cos(PI*fraction))).toFloat()
                if (fraction < 1) main.postDelayed(this,8) else done()
            }
        }
        main.post(tick)
    }

    private fun cancelFade() {
        fadeSerial++
        if (pausing) player.pause()
        pausing = false
        player.volume = 1f
    }

    private fun prefetch() {
        if (generation < 0 || player.mediaItemCount == 0) return
        val current = player.currentMediaItemIndex
        val slots = (maxOf(0,current-1)..minOf(current+2,player.mediaItemCount-1))
            .map { player.getMediaItemAt(it).mediaId.toInt() }.toSet()
        filesInUse = sources.files(slots)
        for (index in current..minOf(current + 2, player.mediaItemCount - 1)) {
            sources.request(generation, player.getMediaItemAt(index).mediaId.toInt())
        }
        if (player.repeatMode == Player.REPEAT_MODE_ALL && current + 2 >= player.mediaItemCount) {
            sources.request(generation, player.getMediaItemAt(0).mediaId.toInt())
        }
    }

    fun resolved(generation: Int) {
        if (generation != this.generation) return
        prefetch()
        applyGain()
    }

    fun publish(deliver: Boolean = true, forceCache: Boolean = false) {
        if (applying || released || generation < 0 || player.currentMediaItem == null) return
        val state = player.playbackState
        val duration = player.duration.takeIf { it != C.TIME_UNSET } ?: 0L
        val data = JSONObject().put("generation", generation).put("serial", serial)
            .put("slot", player.currentMediaItem!!.mediaId.toInt())
            .put("position", player.currentPosition).put("duration", duration)
            .put("wantPlay", player.playWhenReady && !pausing && state != Player.STATE_ENDED)
            .put("playing", player.isPlaying).put("buffering", state == Player.STATE_BUFFERING)
            .put("ready", state != Player.STATE_IDLE).put("ended", state == Player.STATE_ENDED)
            .put("error", error).put("buffered", player.bufferedPercentage / 100.0)
            .put("bands", JSONArray(processor.bands.map { it.toDouble() }))
            .put("renderedFrames",processor.renderedFrames).put("audioUnderruns",underruns)
            .put("outputVolume", player.volume.toDouble())
        sources.get(generation,player.currentMediaItem!!.mediaId.toInt())?.let { data.put("stream",it.data) }
        val revision = cache.revision.get()
        if (forceCache || revision != cacheRevision) {
            data.put("cache",cache.status())
            if (deliver) cacheRevision = revision
        }
        snapshot = data.toString()
        if (deliver) PlayerWebRuntime.evaluate("globalThis.__mygoNativeState?.(${JSONObject.quote(snapshot)})")
    }

    override fun onDestroy() {
        released = true
        main.removeCallbacksAndMessages(null)
        sources.reset()
        session.release()
        player.release()
        NativePlayback.detached(this)
        super.onDestroy()
    }
}
