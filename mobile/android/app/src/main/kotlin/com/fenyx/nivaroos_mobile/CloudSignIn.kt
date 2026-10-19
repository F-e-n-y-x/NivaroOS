package com.fenyx.nivaroos_mobile

import android.app.Activity
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.graphics.drawable.Icon
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log

// Cloud drive sign-in in the browser (lib/services/cloud_signin.dart).

// "Return to NivaroOS" on the page the phone shows after the provider's
// redirect (nivaroos-cloud:done). Brings the app's own task back and closes
// the browser tab sitting on top of it (CLEAR_TOP), the AppAuth way. Takes
// nothing from the link.
class CloudSignInReturnActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startActivity(
            Intent(this, MainActivity::class.java).addFlags(
                Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP,
            ),
        )
        finish()
    }
}

// Keeps the app's process running (not frozen as a cached app) while the
// browser covers it, so its 127.0.0.1:53682 listener can take the
// provider's redirect. Started when the user presses Sign in, stopped when
// the sign-in ends, and after 11 minutes at the latest (the server gives up
// after 10).
class CloudSignInService : Service() {
    companion object {
        private const val TAG = "CloudSignIn"
        private const val CHANNEL_ID = "cloud_signin"
        private const val NOTIFICATION_ID = 42851
        private const val LIMIT_MS = 11 * 60 * 1000L

        fun start(context: Context, provider: String) {
            val i = Intent(context, CloudSignInService::class.java).putExtra("provider", provider)
            try {
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) context.startForegroundService(i) else context.startService(i)
            } catch (e: Exception) {
                Log.w(TAG, "not started: ${e.javaClass.simpleName}")
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, CloudSignInService::class.java))
        }
    }

    private val handler = Handler(Looper.getMainLooper())

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val provider = intent?.getStringExtra("provider")?.take(40) ?: "your cloud drive"
        try {
            val n = notification(provider)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                startForeground(NOTIFICATION_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
            } else {
                startForeground(NOTIFICATION_ID, n)
            }
        } catch (e: Exception) {
            // Not allowed now (the day's dataSync time is used up): the
            // sign-in still works while the app isn't frozen.
            Log.w(TAG, "startForeground refused: ${e.javaClass.simpleName}")
            stopSelf()
            return START_NOT_STICKY
        }
        handler.removeCallbacksAndMessages(null)
        handler.postDelayed({ stopSelf() }, LIMIT_MS)
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        handler.removeCallbacksAndMessages(null)
        super.onDestroy()
    }

    private fun notification(provider: String): Notification {
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val nm = getSystemService(NotificationManager::class.java)
            if (nm?.getNotificationChannel(CHANNEL_ID) == null) {
                nm?.createNotificationChannel(NotificationChannel(CHANNEL_ID, "Cloud drive sign-in", NotificationManager.IMPORTANCE_LOW).apply {
                    description = "Shown while you sign in to a cloud drive in the browser"
                    setShowBadge(false)
                })
            }
            Notification.Builder(this, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        return builder
            .setSmallIcon(Icon.createWithResource(this, R.drawable.ic_stat_nivaroos))
            .setContentTitle("Signing in to $provider")
            .setContentText("Finish in the browser, then return to NivaroOS")
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setCategory(Notification.CATEGORY_SERVICE)
            .build()
    }
}
