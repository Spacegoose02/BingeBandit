package com.bingebandit.tv

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioTrack
import android.media.MediaCodecList
import android.os.Build
import com.facebook.react.bridge.Arguments
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod

/**
 * Tells the player which surround formats this device can actually play: a
 * decoder on the box, or passthrough to the TV/receiver over HDMI. Without
 * either, ExoPlayer plays the video silently instead of failing, so the
 * player has to know before choosing direct play.
 */
class AudioSupportModule(private val ctx: ReactApplicationContext) : ReactContextBaseJavaModule(ctx) {
  override fun getName() = "AudioSupport"

  // ffprobe codec name -> decoder MIME type and passthrough encoding.
  private val formats = listOf(
    Triple("ac3", "audio/ac3", AudioFormat.ENCODING_AC3),
    Triple("eac3", "audio/eac3", AudioFormat.ENCODING_E_AC3),
    Triple("dts", "audio/vnd.dts", AudioFormat.ENCODING_DTS),
    Triple("truehd", "audio/true-hd", if (Build.VERSION.SDK_INT >= 28) AudioFormat.ENCODING_DOLBY_TRUEHD else -1),
  )

  /** Resolves to the ffprobe codec names (of ac3, eac3, dts, truehd) this device can play. */
  @ReactMethod
  fun playableCodecs(promise: Promise) {
    try {
      val decoders = MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos
        .filter { !it.isEncoder }
        .flatMap { it.supportedTypes.map { t -> t.lowercase() } }
        .toSet()
      val out = Arguments.createArray()
      for ((codec, mime, encoding) in formats) {
        if (mime in decoders || (encoding > 0 && passthrough(encoding))) out.pushString(codec)
      }
      promise.resolve(out)
    } catch (e: Exception) {
      promise.reject("audio_support", e)
    }
  }

  @Suppress("DEPRECATION") // isDirectPlaybackSupported: the newer call needs API 33
  private fun passthrough(encoding: Int): Boolean {
    if (Build.VERSION.SDK_INT >= 29) {
      val format = AudioFormat.Builder()
        .setEncoding(encoding)
        .setSampleRate(48000)
        .setChannelMask(AudioFormat.CHANNEL_OUT_5POINT1)
        .build()
      val attrs = AudioAttributes.Builder()
        .setUsage(AudioAttributes.USAGE_MEDIA)
        .setContentType(AudioAttributes.CONTENT_TYPE_MOVIE)
        .build()
      return AudioTrack.isDirectPlaybackSupported(format, attrs)
    }
    val am = ctx.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    return am.getDevices(AudioManager.GET_DEVICES_OUTPUTS).any {
      (it.type == AudioDeviceInfo.TYPE_HDMI || it.type == AudioDeviceInfo.TYPE_HDMI_ARC) && encoding in it.encodings
    }
  }
}
