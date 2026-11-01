package com.bingebandit.tv

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder

/**
 * A foreground service with a "Downloading…" notification, so Android keeps
 * the app running while offline downloads continue with the screen off. The
 * downloading itself happens in DownloadsModule; the app stops this service
 * when the queue is empty.
 */
class DownloadService : Service() {
  override fun onBind(intent: Intent?): IBinder? = null

  override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
    val title = intent?.getStringExtra("title")?.takeIf { it.isNotEmpty() } ?: "Downloading"
    val channel = "downloads"
    if (Build.VERSION.SDK_INT >= 26) {
      val nm = getSystemService(NotificationManager::class.java)
      nm.createNotificationChannel(NotificationChannel(channel, "Downloads", NotificationManager.IMPORTANCE_LOW))
    }
    val open = PendingIntent.getActivity(
      this, 0,
      Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
      PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )
    val builder = if (Build.VERSION.SDK_INT >= 26) Notification.Builder(this, channel) else @Suppress("DEPRECATION") Notification.Builder(this)
    val n = builder
      .setContentTitle("BingeBandit")
      .setContentText(title)
      .setSmallIcon(android.R.drawable.stat_sys_download)
      .setContentIntent(open)
      .setOngoing(true)
      .build()
    try {
      if (Build.VERSION.SDK_INT >= 29) startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
      else startForeground(1, n)
    } catch (e: Exception) {
      stopSelf()
    }
    return START_NOT_STICKY
  }
}
