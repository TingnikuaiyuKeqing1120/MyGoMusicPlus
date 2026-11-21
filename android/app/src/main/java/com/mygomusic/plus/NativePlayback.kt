package com.mygomusic.plus

import android.content.Context
import android.content.Intent
import android.os.Handler
import android.os.Looper
import org.json.JSONObject
import java.util.concurrent.Executors

// Commands are kept in-process, so a large queue does not cross Android's
// Intent transaction limit. All player calls run on the main looper.
object NativePlayback {
    private val main = Handler(Looper.getMainLooper())
    private val pending = ArrayDeque<JSONObject>()
    private val resolver = Executors.newSingleThreadExecutor()
    @Volatile
    var service: PlaybackService? = null
        private set

    fun command(context: Context, text: String) {
        val command = JSONObject(text)
        main.post {
            val active = service
            if (active != null) active.command(command)
            else if (command.optString("action") == "queue") {
                pending.add(command)
                val intent = Intent(context, PlaybackService::class.java)
                if (command.optBoolean("play")) context.startForegroundService(intent)
                else context.startService(intent)
            } else if (pending.isNotEmpty()) pending.add(command)
        }
    }

    fun attached(value: PlaybackService) {
        service = value
        while (pending.isNotEmpty()) value.command(pending.removeFirst())
    }

    fun detached(value: PlaybackService) {
        if (service === value) service = null
        PlayerWebRuntime.releaseIfIdle()
    }

    fun resolved(text: String) {
        val result = JSONObject(text)
        resolver.execute {
            val active = service
            active?.sources?.complete(result)
            main.post { if (service === active) active?.resolved(result.getInt("generation")) }
        }
    }

    fun filesInUse(): Set<String> = service?.filesInUse ?: emptySet()
}
