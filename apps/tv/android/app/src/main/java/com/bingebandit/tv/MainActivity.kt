package com.bingebandit.tv

import android.content.Context
import android.content.res.Configuration
import android.os.Build
import com.facebook.react.ReactActivity
import com.facebook.react.ReactActivityDelegate
import com.facebook.react.defaults.DefaultNewArchitectureEntryPoint.fabricEnabled
import com.facebook.react.defaults.DefaultReactActivityDelegate

class MainActivity : ReactActivity() {

  /**
   * Returns the name of the main component registered from JavaScript. This is used to schedule
   * rendering of the component.
   */
  override fun getMainComponentName(): String = "BingeBanditTV"

  /**
   * Returns the instance of the [ReactActivityDelegate]. We use [DefaultReactActivityDelegate]
   * which allows you to enable New Architecture with a single boolean flags [fabricEnabled]
   */
  override fun createReactActivityDelegate(): ReactActivityDelegate =
      DefaultReactActivityDelegate(this, mainComponentName, fabricEnabled)

  /**
   * Android's "Bold text" setting (fontWeightAdjustment) makes views draw text
   * bolder than React Native measured it, so the last word of many labels wraps
   * onto a hidden line. Draw at the weights the app asks for instead; font size
   * settings still apply.
   */
  override fun attachBaseContext(newBase: Context) {
    if (Build.VERSION.SDK_INT >= 31 && newBase.resources.configuration.fontWeightAdjustment != 0) {
      val config = Configuration(newBase.resources.configuration).apply { fontWeightAdjustment = 0 }
      super.attachBaseContext(newBase.createConfigurationContext(config))
    } else {
      super.attachBaseContext(newBase)
    }
  }
}
