package com.bingebandit.tv

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import com.facebook.react.bridge.Arguments
import com.facebook.react.bridge.Promise
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod

/**
 * "Open BingeBandit when this TV starts": a switch in Settings, kept on the
 * device. When it's on, [BootReceiver] opens the app once the device has
 * finished starting up.
 *
 * Android 10 and later only let an app open itself from the background when
 * it may "display over other apps" (SYSTEM_ALERT_WINDOW). Fire TV and most
 * Android TV boxes have that page in Settings; on Google TV and Chromecast it's
 * hidden, so it's granted once with adb:
 *   adb shell appops set com.bingebandit.tv SYSTEM_ALERT_WINDOW allow
 * The Play build doesn't ask for the permission, so there the switch only
 * works on Android 9 and older.
 */
object AutoLaunch {
  private const val PREFS = "auto_launch"
  private const val KEY_ON = "on"

  fun isOn(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(KEY_ON, false)

  fun setOn(ctx: Context, on: Boolean) {
    ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(KEY_ON, on).apply()
  }

  /** Whether Android needs "display over other apps" before the app may open itself at start-up. */
  fun needsPermission(ctx: Context) = Build.VERSION.SDK_INT >= 29 && !Settings.canDrawOverlays(ctx)

  /** Whether this build asks for that permission at all (the Play build doesn't). */
  fun canAsk(ctx: Context): Boolean =
    try {
      val info = ctx.packageManager.getPackageInfo(ctx.packageName, PackageManager.GET_PERMISSIONS)
      info.requestedPermissions?.contains(Manifest.permission.SYSTEM_ALERT_WINDOW) == true
    } catch (e: Exception) {
      false
    }

  fun open(ctx: Context) {
    val intent = ctx.packageManager.getLaunchIntentForPackage(ctx.packageName)
      ?: Intent(ctx, MainActivity::class.java)
    intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_RESET_TASK_IF_NEEDED)
    ctx.startActivity(intent)
  }
}

/** Opens the app when the device finishes starting, if the switch is on. */
class BootReceiver : BroadcastReceiver() {
  override fun onReceive(ctx: Context, intent: Intent) {
    val action = intent.action ?: return
    if (action != Intent.ACTION_BOOT_COMPLETED && action != "android.intent.action.QUICKBOOT_POWERON") return
    if (!AutoLaunch.isOn(ctx)) return
    try {
      AutoLaunch.open(ctx)
    } catch (e: Exception) {
      // Android refused (no "display over other apps"): the TV starts on its home screen as before.
    }
  }
}

/** Settings' switch for [AutoLaunch]. */
class AutoLaunchModule(private val ctx: ReactApplicationContext) : ReactContextBaseJavaModule(ctx) {
  override fun getName() = "AutoLaunch"

  /** { on, needsPermission, canAsk } */
  @ReactMethod
  fun state(promise: Promise) {
    val map = Arguments.createMap()
    map.putBoolean("on", AutoLaunch.isOn(ctx))
    map.putBoolean("needsPermission", AutoLaunch.needsPermission(ctx))
    map.putBoolean("canAsk", AutoLaunch.canAsk(ctx))
    promise.resolve(map)
  }

  @ReactMethod
  fun setOn(on: Boolean) {
    AutoLaunch.setOn(ctx, on)
  }

  /** Opens Android's "display over other apps" page; false when this device hides it. */
  @ReactMethod
  fun openPermission(promise: Promise) {
    val tries = listOf(
      Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION, Uri.parse("package:${ctx.packageName}")),
      Intent(Settings.ACTION_MANAGE_OVERLAY_PERMISSION),
    )
    for (intent in tries) {
      intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
      try {
        (ctx.currentActivity ?: ctx).startActivity(intent)
        promise.resolve(true)
        return
      } catch (e: ActivityNotFoundException) {
      } catch (e: SecurityException) {
      }
    }
    promise.resolve(false)
  }
}
