package com.bingebandit.tv

import android.content.ContentUris
import android.content.Context
import android.net.Uri
import android.os.Build
import android.util.Log
import androidx.tvprovider.media.tv.TvContractCompat
import androidx.tvprovider.media.tv.WatchNextProgram
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.bridge.ReadableArray
import com.facebook.react.bridge.ReadableMap

/**
 * Continue Watching on the Android TV / Google TV home screen ("Watch Next"
 * row). The app hands over what's in its own Continue Watching each time
 * Home loads; this keeps one program per title in the system's row, updates
 * the position, and takes away what's no longer there. Choosing one opens
 * bingebandit://play/<id>?t=<seconds>.
 *
 * A program the viewer removed from the row (long press, Remove) isn't put
 * back until they watch more of it.
 */
class WatchNextModule(private val ctx: ReactApplicationContext) : ReactContextBaseJavaModule(ctx) {
  override fun getName() = "WatchNext"

  // Our item id -> the system's program id.
  private val ids get() = ctx.getSharedPreferences("watch_next", Context.MODE_PRIVATE)
  // Our item id -> the position (ms) it had when the viewer removed it.
  private val dismissed get() = ctx.getSharedPreferences("watch_next_dismissed", Context.MODE_PRIVATE)

  private fun supported() =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && ctx.packageManager.hasSystemFeature("android.software.leanback")

  @ReactMethod
  fun isSupported(promise: Promise) = promise.resolve(supported())

  @ReactMethod
  fun sync(items: ReadableArray, promise: Promise) {
    if (!supported()) {
      promise.resolve(false)
      return
    }
    Thread {
      try {
        val keep = HashSet<String>()
        for (i in 0 until items.size()) {
          val m = items.getMap(i) ?: continue
          val id = m.getString("id") ?: continue
          keep.add(id)
          upsert(id, m)
        }
        for (key in ids.all.keys.toList()) {
          if (key !in keep) remove(key)
        }
        for (key in dismissed.all.keys.toList()) {
          if (key !in keep) dismissed.edit().remove(key).apply()
        }
        promise.resolve(true)
      } catch (e: Exception) {
        // A launcher without the provider (some Fire TV builds) just has no row.
        Log.w("WatchNext", "sync failed", e)
        promise.resolve(false)
      }
    }.start()
  }

  @ReactMethod
  fun clear(promise: Promise) {
    if (!supported()) {
      promise.resolve(false)
      return
    }
    Thread {
      try {
        for (key in ids.all.keys.toList()) remove(key)
        dismissed.edit().clear().apply()
      } catch (e: Exception) {
        Log.w("WatchNext", "clear failed", e)
      }
      promise.resolve(true)
    }.start()
  }

  private fun existing(programId: Long): WatchNextProgram? {
    ctx.contentResolver.query(TvContractCompat.buildWatchNextProgramUri(programId), null, null, null, null)?.use { c ->
      if (c.moveToFirst()) return WatchNextProgram.fromCursor(c)
    }
    return null
  }

  private fun remove(key: String) {
    val programId = ids.getLong(key, -1)
    if (programId >= 0) {
      try {
        ctx.contentResolver.delete(TvContractCompat.buildWatchNextProgramUri(programId), null, null)
      } catch (e: Exception) {
        Log.w("WatchNext", "delete failed", e)
      }
    }
    ids.edit().remove(key).apply()
  }

  private fun upsert(id: String, m: ReadableMap) {
    val position = if (m.hasKey("position")) m.getDouble("position").toLong() else 0L
    val duration = if (m.hasKey("duration")) m.getDouble("duration").toLong() else 0L
    val known = ids.getLong(id, -1)
    if (known >= 0) {
      val prog = existing(known)
      if (prog == null || !prog.isBrowsable) {
        // Gone, or the viewer removed it: remember where, and don't bring it back until they watch on.
        if (prog != null) ctx.contentResolver.delete(TvContractCompat.buildWatchNextProgramUri(known), null, null)
        ids.edit().remove(id).apply()
        if (prog != null) dismissed.edit().putLong(id, position).apply()
        return
      }
    }
    val was = dismissed.getLong(id, -1)
    if (was >= 0) {
      if (Math.abs(position - was) < 60_000) return
      dismissed.edit().remove(id).apply()
    }
    val episode = m.hasKey("episode") && !m.isNull("episode")
    val b = WatchNextProgram.Builder()
      .setType(if (episode) TvContractCompat.PreviewProgramColumns.TYPE_TV_EPISODE else TvContractCompat.PreviewProgramColumns.TYPE_MOVIE)
      .setWatchNextType(
        if (position > 0) TvContractCompat.WatchNextPrograms.WATCH_NEXT_TYPE_CONTINUE
        else TvContractCompat.WatchNextPrograms.WATCH_NEXT_TYPE_NEXT
      )
      .setLastEngagementTimeUtcMillis(if (m.hasKey("lastWatched")) m.getDouble("lastWatched").toLong() else System.currentTimeMillis())
      .setTitle(m.getString("title") ?: "")
      .setInternalProviderId(id)
      .setIntentUri(Uri.parse("bingebandit://play/$id?t=${position / 1000}"))
    if (m.hasKey("description")) b.setDescription(m.getString("description"))
    if (episode) {
      b.setEpisodeTitle(m.getString("episode"))
      if (m.hasKey("seasonNumber")) b.setSeasonNumber(m.getInt("seasonNumber"))
      if (m.hasKey("episodeNumber")) b.setEpisodeNumber(m.getInt("episodeNumber"))
    }
    if (m.hasKey("image") && !m.isNull("image")) {
      b.setPosterArtUri(Uri.parse(m.getString("image")))
        .setPosterArtAspectRatio(TvContractCompat.PreviewProgramColumns.ASPECT_RATIO_16_9)
    }
    if (duration > 0) {
      b.setDurationMillis(duration.toInt())
      b.setLastPlaybackPositionMillis(position.toInt())
    }
    val values = b.build().toContentValues()
    if (known >= 0) {
      ctx.contentResolver.update(TvContractCompat.buildWatchNextProgramUri(known), values, null, null)
    } else {
      val uri = ctx.contentResolver.insert(TvContractCompat.WatchNextPrograms.CONTENT_URI, values) ?: return
      ids.edit().putLong(id, ContentUris.parseId(uri)).apply()
    }
  }
}
