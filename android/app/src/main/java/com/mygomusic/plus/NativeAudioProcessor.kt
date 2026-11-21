package com.mygomusic.plus

import androidx.media3.common.C
import androidx.media3.common.audio.AudioProcessor.AudioFormat
import androidx.media3.common.audio.BaseAudioProcessor
import java.nio.ByteBuffer
import kotlin.math.*

// Gain and spectrum measurement run on ExoPlayer's audio thread. The UI reads
// small level snapshots, without participating in audio delivery.
class NativeAudioProcessor : BaseAudioProcessor() {
    @Volatile var gain = 1.0
    @Volatile var bands = FloatArray(24)
        private set
    @Volatile var renderedFrames = 0L
        private set
    @Volatile var measureSpectrum = true
    private var currentGain = 1.0
    private var channels = 2
    private var rate = 48000
    private val real = DoubleArray(1024)
    private val imag = DoubleArray(1024)
    private val window = DoubleArray(1024) { 0.5-0.5*cos(2*PI*it/1023) }
    private val samples = DoubleArray(1024)
    private var filled = 0

    override fun onConfigure(inputAudioFormat: AudioFormat): AudioFormat {
        if (inputAudioFormat.encoding != C.ENCODING_PCM_16BIT) {
            throw androidx.media3.common.audio.AudioProcessor.UnhandledAudioFormatException(inputAudioFormat)
        }
        channels = inputAudioFormat.channelCount
        rate = inputAudioFormat.sampleRate
        return inputAudioFormat
    }

    override fun queueInput(inputBuffer: ByteBuffer) {
        val frames = inputBuffer.remaining() / (channels * 2)
        val output = replaceOutputBuffer(inputBuffer.remaining())
        val change = 1.0 / (rate * 0.005)
        while (inputBuffer.remaining() >= channels * 2) {
            currentGain += (gain-currentGain).coerceIn(-change, change)
            var mono = 0.0
            repeat(channels) {
                val sample = inputBuffer.short.toInt()
                mono += sample.toDouble()/32768
                output.putShort((sample*currentGain).roundToInt().coerceIn(-32768,32767).toShort())
            }
            if (measureSpectrum) {
                samples[filled++] = mono/channels
                if (filled == samples.size) { analyze(); filled = 0 }
            }
        }
        renderedFrames += frames
        output.flip()
    }

    override fun onFlush() { filled = 0; bands = FloatArray(24) }

    private fun analyze() {
        for (i in real.indices) { real[i] = samples[i]*window[i]; imag[i] = 0.0 }
        var j = 0
        for (i in 1 until real.size) {
            var bit = real.size shr 1
            while ((j and bit) != 0) { j = j xor bit; bit = bit shr 1 }
            j = j xor bit
            if (i < j) { val value = real[i]; real[i] = real[j]; real[j] = value }
        }
        var length = 2
        while (length <= real.size) {
            val angle = -2*PI/length
            for (start in real.indices step length) {
                for (k in 0 until length/2) {
                    val a = start+k
                    val b = a+length/2
                    val wr = cos(angle*k)
                    val wi = sin(angle*k)
                    val r = wr*real[b]-wi*imag[b]
                    val im = wr*imag[b]+wi*real[b]
                    real[b] = real[a]-r; imag[b] = imag[a]-im
                    real[a] += r; imag[a] += im
                }
            }
            length *= 2
        }
        val levels = FloatArray(24)
        val lo = ln(40.0)
        val hi = ln(min(14000.0,rate/2.0))
        for (band in levels.indices) {
            val begin = (exp(lo+(hi-lo)*band/24)*1024/rate).toInt().coerceIn(1,511)
            val end = (exp(lo+(hi-lo)*(band+1)/24)*1024/rate).toInt().coerceIn(begin+1,512)
            var sum = 0.0
            for (k in begin until end) sum += hypot(real[k],imag[k])
            val magnitude = sum/(end-begin)/256*(1.0+band/6.0)
            levels[band] = ((20*log10(magnitude+1e-9)+60)/52).coerceIn(0.0,1.0).toFloat()
        }
        bands = levels
    }
}
