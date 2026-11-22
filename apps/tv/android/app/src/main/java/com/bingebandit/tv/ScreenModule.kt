package com.bingebandit.tv

import android.content.pm.ActivityInfo
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.bridge.ReactContextBaseJavaModule
import com.facebook.react.bridge.ReactMethod
import com.facebook.react.bridge.UiThreadUtil

/** Lets the players turn a phone sideways while video plays, and hand rotation back after. */
class ScreenModule(private val ctx: ReactApplicationContext) : ReactContextBaseJavaModule(ctx) {
  override fun getName() = "Screen"

  @ReactMethod
  fun setLandscape(on: Boolean) {
    val activity = ctx.currentActivity ?: return
    UiThreadUtil.runOnUiThread {
      activity.requestedOrientation =
        if (on) ActivityInfo.SCREEN_ORIENTATION_SENSOR_LANDSCAPE else ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
    }
  }

  /**
   * Hides the navigation bar (and a tablet's taskbar) while video plays, so the
   * player gets the whole screen. A swipe in from the edge shows them for a moment.
   */
  @ReactMethod
  fun setImmersive(on: Boolean) {
    val activity = ctx.currentActivity ?: return
    UiThreadUtil.runOnUiThread {
      val window = activity.window ?: return@runOnUiThread
      val bars = WindowCompat.getInsetsController(window, window.decorView)
      if (on) {
        bars.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        bars.hide(WindowInsetsCompat.Type.navigationBars())
      } else {
        bars.show(WindowInsetsCompat.Type.navigationBars())
      }
    }
  }

  /**
   * Pins the app to the screen while a player's child lock is on, so the home
   * gesture, the taskbar and recent apps can't leave it. Android asks the person
   * to confirm pinning; unpinning (hold Back and Overview, or swipe up and hold)
   * asks for the device PIN when "Ask for PIN before unpinning" is on.
   */
  @ReactMethod
  fun setPinned(on: Boolean) {
    val activity = ctx.currentActivity ?: return
    UiThreadUtil.runOnUiThread {
      try {
        if (on) activity.startLockTask() else activity.stopLockTask()
      } catch (_: Exception) {
        // Not pinned (or pinning not allowed): the child lock still blocks taps and Back.
      }
    }
  }
}
