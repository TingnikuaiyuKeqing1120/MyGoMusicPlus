package com.mygomusic.plus

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.os.Build
import android.os.IBinder
import android.support.v4.media.MediaMetadataCompat
import android.support.v4.media.session.MediaSessionCompat
import android.support.v4.media.session.PlaybackStateCompat
import androidx.core.app.NotificationCompat
import androidx.media.app.NotificationCompat.MediaStyle
import org.json.JSONObject

// PlaybackService keeps music alive in the background: a mediaPlayback
// foreground service with a media notification and a MediaSession, plus audio
// focus. Transport controls and focus changes drive the wasm app through
// WebLink (the web media-bridge's recorded mediaSession handlers).
class PlaybackService : Service() {

    private lateinit var session: MediaSessionCompat
    private lateinit var audio: AudioManager
    private var focusReq: AudioFocusRequest? = null
    private var playing = false
    private var meta = MediaMetadataCompat.Builder().build()
    private var positionMs = 0L
    private var durationMs = 0L

    companion object {
        private const val CHANNEL = "playback"
        private const val NOTIF = 1
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        audio = getSystemService(AUDIO_SERVICE) as AudioManager
        val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, "Playback", NotificationManager.IMPORTANCE_LOW)
        )
        session = MediaSessionCompat(this, "MyGoPlus").apply {
            setCallback(object : MediaSessionCompat.Callback() {
                override fun onPlay() = WebLink.send("play")
                override fun onPause() = WebLink.send("pause")
                override fun onSkipToNext() = WebLink.send("nexttrack")
                override fun onSkipToPrevious() = WebLink.send("previoustrack")
                override fun onSeekTo(pos: Long) = WebLink.send("seekto")
                override fun onStop() {
                    WebLink.send("pause")
                    stopForeground(STOP_FOREGROUND_REMOVE)
                    stopSelf()
                }
            })
            isActive = true
        }
        val attrs = AudioAttributes.Builder()
            .setUsage(AudioAttributes.USAGE_MEDIA)
            .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
            .build()
        focusReq = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
            .setAudioAttributes(attrs)
            .setOnAudioFocusChangeListener { change ->
                when (change) {
                    AudioManager.AUDIOFOCUS_LOSS,
                    AudioManager.AUDIOFOCUS_LOSS_TRANSIENT -> WebLink.send("pause")
                }
            }
            .build()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.getIntExtra("ctl", 0)) {
            1 -> WebLink.send("pause")
            2 -> WebLink.send("nexttrack")
        }
        intent?.getStringExtra("meta")?.let { s ->
            if (s.isEmpty()) return@let
            val j = JSONObject(s)
            meta = MediaMetadataCompat.Builder()
                .putString(MediaMetadataCompat.METADATA_KEY_TITLE, j.optString("title"))
                .putString(MediaMetadataCompat.METADATA_KEY_ARTIST, j.optString("artist"))
                .putString(MediaMetadataCompat.METADATA_KEY_ALBUM, j.optString("album"))
                .putLong(MediaMetadataCompat.METADATA_KEY_DURATION, durationMs)
                .build()
            session.setMetadata(meta)
        }
        intent?.getStringExtra("pos")?.let { s ->
            val j = JSONObject(s)
            positionMs = (j.optDouble("position") * 1000).toLong()
            durationMs = (j.optDouble("duration") * 1000).toLong()
        }
        intent?.getStringExtra("playing")?.let { s ->
            val now = s == "1"
            if (now && !playing) {
                focusReq?.let { audio.requestAudioFocus(it) }
            } else if (!now && playing) {
                focusReq?.let { audio.abandonAudioFocusRequest(it) }
            }
            playing = now
        }
        session.setPlaybackState(
            PlaybackStateCompat.Builder()
                .setActions(
                    PlaybackStateCompat.ACTION_PLAY or PlaybackStateCompat.ACTION_PAUSE or
                        PlaybackStateCompat.ACTION_PLAY_PAUSE or
                        PlaybackStateCompat.ACTION_SKIP_TO_NEXT or
                        PlaybackStateCompat.ACTION_SKIP_TO_PREVIOUS or
                        PlaybackStateCompat.ACTION_SEEK_TO or PlaybackStateCompat.ACTION_STOP
                )
                .setState(
                    if (playing) PlaybackStateCompat.STATE_PLAYING else PlaybackStateCompat.STATE_PAUSED,
                    positionMs, 1f
                )
                .build()
        )
        val n = buildNotification()
        if (playing) startForeground(NOTIF, n)
        else {
            stopForeground(STOP_FOREGROUND_DETACH)
            getSystemService(NotificationManager::class.java).notify(NOTIF, n)
        }
        return START_STICKY
    }

    private fun buildNotification(): Notification {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        fun action(code: Int, icon: Int, title: String): NotificationCompat.Action {
            val i = Intent(this, PlaybackService::class.java).putExtra("ctl", code)
            val pi = PendingIntent.getService(
                this, code, i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
            return NotificationCompat.Action(icon, title, pi)
        }
        val b = NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(android.R.drawable.ic_media_play)
            .setContentTitle(meta.getString(MediaMetadataCompat.METADATA_KEY_TITLE) ?: "MyGo Music++")
            .setContentText(meta.getString(MediaMetadataCompat.METADATA_KEY_ARTIST) ?: "")
            .setContentIntent(open)
            .setOngoing(playing)
            .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
            .addAction(action(1, android.R.drawable.ic_media_pause, "Pause"))
            .addAction(action(2, android.R.drawable.ic_media_next, "Next"))
            .setStyle(MediaStyle().setMediaSession(session.sessionToken).setShowActionsInCompactView(0, 1))
        return b.build()
    }

    override fun onDestroy() {
        focusReq?.let { audio.abandonAudioFocusRequest(it) }
        session.release()
        super.onDestroy()
    }
}