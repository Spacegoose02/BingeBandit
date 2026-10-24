package com.bingebandit.tv

import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import com.facebook.react.bridge.Arguments
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.modules.core.DeviceEventManagerModule
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

/**
 * Updates the app from a release APK: reports the installed version, downloads
 * the new APK into the cache (sending "AppUpdaterProgress" events) and hands it
 * to Android's package installer, which asks the viewer to confirm.
 */
class AppUpdaterModule(private val ctx: ReactApplicationContext) : ReactContextBaseJavaModule(ctx) {
  override fun getName() = "AppUpdater"

  override fun getConstants(): Map<String, Any> {
    val info = ctx.packageManager.getPackageInfo(ctx.packageName, 0)
    val code = if (Build.VERSION.SDK_INT >= 28) info.longVersionCode else @Suppress("DEPRECATION") info.versionCode.toLong()
    return mapOf("versionName" to (info.versionName ?: ""), "versionCode" to code.toDouble())
  }

  /** Whether Android lets this app install packages (the "install unknown apps" switch). */
  @ReactMethod
  fun canInstall(promise: Promise) {
    promise.resolve(Build.VERSION.SDK_INT < 26 || ctx.packageManager.canRequestPackageInstalls())
  }

  /** Opens the system screen where the viewer allows this app to install updates. */
  @ReactMethod
  fun openInstallSettings(promise: Promise) {
    try {
      val intent = if (Build.VERSION.SDK_INT >= 26)
        Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:" + ctx.packageName))
      else Intent(Settings.ACTION_SECURITY_SETTINGS)
      intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
      ctx.startActivity(intent)
      promise.resolve(true)
    } catch (e: Exception) {
      promise.reject("settings", "This TV doesn't have that settings screen: ${e.message}")
    }
  }

  @ReactMethod
  fun downloadAndInstall(url: String, promise: Promise) {
    Thread {
      try {
        val dir = File(ctx.cacheDir, "updates").apply { mkdirs() }
        dir.listFiles()?.forEach { it.delete() }
        val file = File(dir, "BingeBandit-update.apk")
        val conn = URL(url).openConnection() as HttpURLConnection
        conn.instanceFollowRedirects = true
        conn.connectTimeout = 20000
        conn.readTimeout = 30000
        if (conn.responseCode !in 200..299) throw Exception("download failed (${conn.responseCode})")
        val total = conn.contentLengthLong
        var done = 0L
        var lastSent = 0L
        conn.inputStream.use { input ->
          file.outputStream().use { out ->
            val buf = ByteArray(64 * 1024)
            while (true) {
              val n = input.read(buf)
              if (n < 0) break
              out.write(buf, 0, n)
              done += n
              if (done - lastSent > 512 * 1024 || done == total) {
                lastSent = done
                val ev = Arguments.createMap()
                ev.putDouble("done", done.toDouble())
                ev.putDouble("total", total.toDouble())
                ctx.getJSModule(DeviceEventManagerModule.RCTDeviceEventEmitter::class.java).emit("AppUpdaterProgress", ev)
              }
            }
          }
        }
        val uri = FileProvider.getUriForFile(ctx, ctx.packageName + ".updates", file)
        val intent = Intent(Intent.ACTION_VIEW)
          .setDataAndType(uri, "application/vnd.android.package-archive")
          .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
        ctx.startActivity(intent)
        promise.resolve(true)
      } catch (e: Exception) {
        promise.reject("update", e.message ?: "update failed", e)
      }
    }.start()
  }

  // Required by NativeEventEmitter on the JS side.
  @ReactMethod fun addListener(eventName: String) {}
  @ReactMethod fun removeListeners(count: Double) {}
}
