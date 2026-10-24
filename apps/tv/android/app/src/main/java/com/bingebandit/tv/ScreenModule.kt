package com.bingebandit.tv

import android.content.pm.ActivityInfo
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
}
