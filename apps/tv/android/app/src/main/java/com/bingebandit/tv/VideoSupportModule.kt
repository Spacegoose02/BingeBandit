package com.bingebandit.tv

import android.media.MediaCodecInfo
import android.media.MediaCodecInfo.CodecProfileLevel
import android.media.MediaCodecList
import android.os.Build
import android.view.Display
import com.facebook.react.bridge.Arguments
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.bridge.UiThreadUtil
import kotlin.math.abs
import kotlin.math.roundToInt

/**
 * What this box can show, so the player picks direct play or conversion up
 * front instead of finding out from a black screen: the video codecs it
 * decodes (how big, and whether 10-bit), and the HDR formats the screen
 * takes. It also switches the screen's refresh rate to the video's frame
 * rate (24p films judder on a 60 Hz screen) and back.
 */
class VideoSupportModule(private val ctx: ReactApplicationContext) : ReactContextBaseJavaModule(ctx) {
  override fun getName() = "VideoSupport"

  // ffprobe codec name -> decoder MIME type, and the profiles that mean 10-bit.
  private val codecs = listOf(
    Triple("h264", "video/avc", setOf(CodecProfileLevel.AVCProfileHigh10)),
    Triple("hevc", "video/hevc", setOf(CodecProfileLevel.HEVCProfileMain10, CodecProfileLevel.HEVCProfileMain10HDR10, CodecProfileLevel.HEVCProfileMain10HDR10Plus)),
    Triple("vp9", "video/x-vnd.on2.vp9", setOf(CodecProfileLevel.VP9Profile2, CodecProfileLevel.VP9Profile2HDR, CodecProfileLevel.VP9Profile2HDR10Plus)),
    Triple("av1", "video/av01", setOf(CodecProfileLevel.AV1ProfileMain10, CodecProfileLevel.AV1ProfileMain10HDR10, CodecProfileLevel.AV1ProfileMain10HDR10Plus)),
  )

  private fun software(info: MediaCodecInfo): Boolean {
    if (Build.VERSION.SDK_INT >= 29) return info.isSoftwareOnly
    val n = info.name.lowercase()
    return n.startsWith("omx.google.") || n.startsWith("c2.android.") || !n.startsWith("omx.") && !n.startsWith("c2.")
  }

  private fun display(): Display? {
    val activity = ctx.currentActivity ?: return null
    @Suppress("DEPRECATION")
    return if (Build.VERSION.SDK_INT >= 30) activity.display else activity.windowManager.defaultDisplay
  }

  /**
   * Resolves to { codecs: { hevc: { maxHeight, tenBit }, … }, dolbyVision,
   * hdr: ["hdr10", "hlg", "dv", "hdr10plus"] }. A software decoder counts up
   * to 1080p only: a TV box can't keep up with more.
   */
  @ReactMethod
  fun capabilities(promise: Promise) {
    try {
      val infos = MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos.filter { !it.isEncoder }
      val out = Arguments.createMap()
      val found = Arguments.createMap()
      for ((name, mime, tenBitProfiles) in codecs) {
        var maxHeight = 0
        var tenBit = false
        for (info in infos) {
          val type = info.supportedTypes.firstOrNull { it.equals(mime, ignoreCase = true) } ?: continue
          val caps = try { info.getCapabilitiesForType(type) } catch (e: Exception) { continue }
          val video = caps.videoCapabilities ?: continue
          var h = when {
            video.isSizeSupported(3840, 2160) -> 2160
            video.isSizeSupported(1920, 1080) -> 1080
            video.isSizeSupported(1280, 720) -> 720
            else -> 480
          }
          if (software(info)) h = minOf(h, 1080)
          if (h > maxHeight) maxHeight = h
          if (caps.profileLevels.any { it.profile in tenBitProfiles }) tenBit = true
        }
        if (maxHeight > 0) {
          val c = Arguments.createMap()
          c.putInt("maxHeight", maxHeight)
          c.putBoolean("tenBit", tenBit)
          found.putMap(name, c)
        }
      }
      out.putMap("codecs", found)
      out.putBoolean("dolbyVision", infos.any { i -> i.supportedTypes.any { it.equals("video/dolby-vision", ignoreCase = true) } })
      val hdr = Arguments.createArray()
      @Suppress("DEPRECATION")
      display()?.hdrCapabilities?.supportedHdrTypes?.forEach {
        when (it) {
          Display.HdrCapabilities.HDR_TYPE_HDR10 -> hdr.pushString("hdr10")
          Display.HdrCapabilities.HDR_TYPE_HLG -> hdr.pushString("hlg")
          Display.HdrCapabilities.HDR_TYPE_DOLBY_VISION -> hdr.pushString("dv")
          Display.HdrCapabilities.HDR_TYPE_HDR10_PLUS -> hdr.pushString("hdr10plus")
        }
      }
      out.putArray("hdr", hdr)
      promise.resolve(out)
    } catch (e: Exception) {
      promise.reject("video_support", e)
    }
  }

  /**
   * Switches the screen to the refresh rate that fits fps (the rate itself,
   * or a whole multiple: 23.976 fits 23.976, 47.952 or 119.88 Hz) at the
   * current resolution. Resolves to the refresh rate picked, or 0 when no
   * mode fits or the screen is already showing one.
   */
  @ReactMethod
  fun matchFrameRate(fps: Double, promise: Promise) {
    UiThreadUtil.runOnUiThread {
      try {
        val activity = ctx.currentActivity
        val d = display()
        if (activity == null || d == null || fps < 1) {
          promise.resolve(0.0)
          return@runOnUiThread
        }
        val current = d.mode
        val fits = { hz: Float ->
          val n = (hz / fps).roundToInt()
          n >= 1 && abs(hz - n * fps) < 0.05 * n
        }
        if (fits(current.refreshRate)) {
          promise.resolve(0.0)
          return@runOnUiThread
        }
        val pick = d.supportedModes
          .filter { it.physicalWidth == current.physicalWidth && it.physicalHeight == current.physicalHeight && fits(it.refreshRate) }
          .minByOrNull { it.refreshRate } // the rate itself before its multiples
        if (pick == null) {
          promise.resolve(0.0)
          return@runOnUiThread
        }
        val attrs = activity.window.attributes
        attrs.preferredDisplayModeId = pick.modeId
        activity.window.attributes = attrs
        promise.resolve(pick.refreshRate.toDouble())
      } catch (e: Exception) {
        promise.reject("frame_rate", e)
      }
    }
  }

  /** Hands the refresh rate back to the system's choice. */
  @ReactMethod
  fun resetFrameRate() {
    UiThreadUtil.runOnUiThread {
      val activity = ctx.currentActivity ?: return@runOnUiThread
      val attrs = activity.window.attributes
      if (attrs.preferredDisplayModeId != 0) {
        attrs.preferredDisplayModeId = 0
        activity.window.attributes = attrs
      }
    }
  }
}
