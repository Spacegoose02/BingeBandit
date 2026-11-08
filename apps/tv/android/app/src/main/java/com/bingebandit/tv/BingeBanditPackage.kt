package com.bingebandit.tv

import com.facebook.react.ReactPackage
import com.facebook.react.bridge.NativeModule
import com.facebook.react.bridge.ReactApplicationContext
import com.facebook.react.uimanager.ViewManager

/** The app's own native modules: in-app updates, screen orientation, audio format support, offline downloads and the home screen's Watch Next row. */
class BingeBanditPackage : ReactPackage {
  override fun createNativeModules(ctx: ReactApplicationContext): List<NativeModule> = listOf(AppUpdaterModule(ctx), ScreenModule(ctx), AudioSupportModule(ctx), DownloadsModule(ctx), WatchNextModule(ctx))

  override fun createViewManagers(ctx: ReactApplicationContext): List<ViewManager<*, *>> = emptyList()
}
