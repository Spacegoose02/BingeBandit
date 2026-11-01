package com.bingebandit.tv

import android.content.Context
import android.content.Intent
import android.net.wifi.WifiManager
import android.os.Build
import android.os.PowerManager
import com.facebook.react.bridge.Arguments
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.modules.core.DeviceEventManagerModule
import java.io.File
import java.io.FileOutputStream
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Offline downloads for phones: saves a file from the server into the app's
 * own storage, resuming a partial file with a Range request. Sends
 * "DownloadProgress" ({id, done, total}) and "DownloadEnd" ({id, ok, paused,
 * error, size}) events. While anything downloads, DownloadService keeps the
 * app running (with a notification) and wake and Wi-Fi locks are held, so a
 * locked phone keeps going.
 */
class DownloadsModule(private val ctx: ReactApplicationContext) : ReactContextBaseJavaModule(ctx) {
  override fun getName() = "Downloads"

  private val running = ConcurrentHashMap<String, AtomicBoolean>() // id → stop flag
  private var wake: PowerManager.WakeLock? = null
  private var wifi: WifiManager.WifiLock? = null

  private fun dir() = File(ctx.filesDir, "downloads").apply { mkdirs() }

  override fun getConstants(): Map<String, Any> = mapOf("dir" to dir().absolutePath)

  /** Bytes free where downloads are kept. */
  @ReactMethod
  fun freeBytes(promise: Promise) {
    promise.resolve(dir().usableSpace.toDouble())
  }

  /** Size of a file in the downloads folder, or -1 when there isn't one. */
  @ReactMethod
  fun fileSize(path: String, promise: Promise) {
    val f = inDir(path)
    promise.resolve(if (f != null && f.exists()) f.length().toDouble() else -1.0)
  }

  @ReactMethod
  fun deleteFile(path: String, promise: Promise) {
    val f = inDir(path)
    promise.resolve(f != null && (!f.exists() || f.delete()))
  }

  @ReactMethod
  fun readText(path: String, promise: Promise) {
    val f = inDir(path)
    if (f == null || !f.exists()) return promise.resolve("")
    try {
      promise.resolve(f.readText())
    } catch (e: Exception) {
      promise.resolve("")
    }
  }

  @ReactMethod
  fun writeText(path: String, text: String, promise: Promise) {
    val f = inDir(path) ?: return promise.reject("path", "outside the downloads folder")
    try {
      f.writeText(text)
      promise.resolve(true)
    } catch (e: Exception) {
      promise.reject("write", e.message ?: "could not save", e)
    }
  }

  /** Every file in the downloads folder, so the app can remove ones it no longer knows. */
  @ReactMethod
  fun listFiles(promise: Promise) {
    val out = Arguments.createArray()
    dir().listFiles()?.forEach { out.pushString(it.absolutePath) }
    promise.resolve(out)
  }

  /**
   * Starts (or resumes) saving url to path. Resolves at once; the outcome
   * comes as a DownloadEnd event.
   */
  @ReactMethod
  fun start(id: String, url: String, token: String, path: String, promise: Promise) {
    val file = inDir(path) ?: return promise.reject("path", "outside the downloads folder")
    if (running.containsKey(id)) return promise.resolve(true)
    val stop = AtomicBoolean(false)
    running[id] = stop
    holdLocks()
    Thread {
      var ok = false
      var error = ""
      try {
        download(id, url, token, file, stop)
        ok = !stop.get()
      } catch (e: Exception) {
        error = e.message ?: "download failed"
      } finally {
        running.remove(id)
        if (running.isEmpty()) releaseLocks()
      }
      val ev = Arguments.createMap()
      ev.putString("id", id)
      ev.putBoolean("ok", ok)
      ev.putBoolean("paused", stop.get())
      ev.putString("error", error)
      ev.putDouble("size", file.length().toDouble())
      emit("DownloadEnd", ev)
    }.start()
    promise.resolve(true)
  }

  /** Stops a download, keeping what has arrived so it can resume. */
  @ReactMethod
  fun pause(id: String, promise: Promise) {
    running[id]?.set(true)
    promise.resolve(true)
  }

  private fun download(id: String, url: String, token: String, file: File, stop: AtomicBoolean) {
    var have = if (file.exists()) file.length() else 0L
    val conn = URL(url).openConnection() as HttpURLConnection
    conn.instanceFollowRedirects = true
    conn.connectTimeout = 20000
    conn.readTimeout = 60000
    if (token.isNotEmpty()) conn.setRequestProperty("Authorization", "Bearer $token")
    if (have > 0) conn.setRequestProperty("Range", "bytes=$have-")
    val code = conn.responseCode
    if (code == 416) { // already complete
      conn.disconnect()
      return
    }
    if (code !in 200..299) {
      val msg = try {
        conn.errorStream?.bufferedReader()?.readText()?.let { Regex("\"error\"\\s*:\\s*\"([^\"]*)\"").find(it)?.groupValues?.get(1) }
      } catch (e: Exception) {
        null
      }
      conn.disconnect()
      throw Exception(msg ?: "the server answered $code")
    }
    if (code != 206) have = 0 // the server sent the whole file: start over
    val total = if (conn.contentLengthLong >= 0) have + conn.contentLengthLong else -1L
    var done = have
    var lastSent = 0L
    conn.inputStream.use { input ->
      FileOutputStream(file, have > 0).use { out ->
        val buf = ByteArray(128 * 1024)
        while (!stop.get()) {
          val n = input.read(buf)
          if (n < 0) break
          out.write(buf, 0, n)
          done += n
          if (done - lastSent >= 1024 * 1024) {
            lastSent = done
            val ev = Arguments.createMap()
            ev.putString("id", id)
            ev.putDouble("done", done.toDouble())
            ev.putDouble("total", total.toDouble())
            emit("DownloadProgress", ev)
          }
        }
      }
    }
    conn.disconnect()
    if (!stop.get() && total > 0 && done < total) throw Exception("the connection dropped")
  }

  /** Keeps the app alive in the background while downloads run (see DownloadService). */
  @ReactMethod
  fun keepAlive(on: Boolean, title: String, promise: Promise) {
    val intent = Intent(ctx, DownloadService::class.java).putExtra("title", title)
    try {
      if (on) {
        if (Build.VERSION.SDK_INT >= 26) ctx.startForegroundService(intent) else ctx.startService(intent)
      } else {
        ctx.stopService(intent)
      }
      promise.resolve(true)
    } catch (e: Exception) {
      // Android won't start it from the background; downloads still run while the app is open.
      promise.resolve(false)
    }
  }

  private fun inDir(path: String): File? {
    val f = File(path).canonicalFile
    return if (f.parentFile?.canonicalPath == dir().canonicalPath) f else null
  }

  @Synchronized
  private fun holdLocks() {
    if (wake == null) {
      val pm = ctx.getSystemService(Context.POWER_SERVICE) as PowerManager
      wake = pm.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "BingeBandit:downloads").apply { acquire(6 * 60 * 60 * 1000L) }
    }
    if (wifi == null) {
      val wm = ctx.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
      @Suppress("DEPRECATION")
      wifi = wm?.createWifiLock(WifiManager.WIFI_MODE_FULL_HIGH_PERF, "BingeBandit:downloads")?.apply { acquire() }
    }
  }

  @Synchronized
  private fun releaseLocks() {
    wake?.let { if (it.isHeld) it.release() }
    wake = null
    wifi?.let { if (it.isHeld) it.release() }
    wifi = null
  }

  private fun emit(name: String, ev: com.facebook.react.bridge.WritableMap) {
    ctx.getJSModule(DeviceEventManagerModule.RCTDeviceEventEmitter::class.java).emit(name, ev)
  }

  // Required by NativeEventEmitter on the JS side.
  @ReactMethod fun addListener(eventName: String) {}
  @ReactMethod fun removeListeners(count: Double) {}
}
