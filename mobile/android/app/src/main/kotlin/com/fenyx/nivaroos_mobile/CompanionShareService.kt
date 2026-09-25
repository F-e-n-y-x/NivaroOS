package com.fenyx.nivaroos_mobile

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.graphics.drawable.Icon
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.text.DateFormat
import java.util.Date

/**
 * "Share this phone's storage with the server" (plan M-18, M-33, WP1-7): a
 * dataSync foreground service that runs for a session the user chose (at
 * most a few hours) and then stops by itself. It hosts a headless Flutter
 * engine running `shareServiceMain`, which serves the phone's shared
 * storage to the server (CompanionFileServer) and keeps the reverse tunnel
 * open.
 *
 * Android 15 rules it follows:
 * - dataSync services get 6 hours per 24; [onTimeout] stops the service at
 *   once when the system says the budget is used up.
 * - It is never started from BOOT_COMPLETED or from the background; only
 *   from the app on screen.
 * - Not exported: nothing outside the app can start or bind it.
 *
 * Its state (running, end time, why the last session ended) is kept in
 * [PREFS] so the app can show it.
 */
class CompanionShareService : Service() {
    companion object {
        private const val TAG = "CompanionShare"
        const val ACTION_START = "com.fenyx.nivaroos.share.START"
        const val ACTION_STOP = "com.fenyx.nivaroos.share.STOP"
        const val EXTRA_END_AT = "end_at"
        const val EXTRA_SERVER = "server"
        const val CHANNEL_ID = "companion_sharing"
        const val NOTIFICATION_ID = 42843
        const val PREFS = "nivaroos_share"

        /** Why the last session ended: stopped, ended, timeout, not_allowed, signed_out. */
        const val KEY_REASON = "last_stop_reason"
        const val KEY_ENDS_AT = "ends_at"

        @Volatile
        var running = false
            private set

        @Volatile
        var endsAt = 0L
            private set

        fun start(context: Context, endAt: Long, server: String) {
            val intent = Intent(context, CompanionShareService::class.java)
                .setAction(ACTION_START)
                .putExtra(EXTRA_END_AT, endAt)
                .putExtra(EXTRA_SERVER, server)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: Context) {
            if (!running) return
            context.startService(Intent(context, CompanionShareService::class.java).setAction(ACTION_STOP))
        }

        fun ensureChannel(context: Context) {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
            val nm = context.getSystemService(NotificationManager::class.java) ?: return
            if (nm.getNotificationChannel(CHANNEL_ID) != null) return
            val channel = NotificationChannel(CHANNEL_ID, "Storage sharing", NotificationManager.IMPORTANCE_LOW)
            channel.description = "Shown while the server can browse this phone's storage"
            channel.setShowBadge(false)
            nm.createNotificationChannel(channel)
        }
    }

    private val handler = Handler(Looper.getMainLooper())
    private val endRunnable = Runnable { stopSharing("ended") }
    private var engine: FlutterEngine? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> {
                val endAt = intent.getLongExtra(EXTRA_END_AT, 0L)
                val server = intent.getStringExtra(EXTRA_SERVER) ?: ""
                if (!goForeground(endAt, server)) return START_NOT_STICKY
                running = true
                endsAt = endAt
                prefs().edit().putLong(KEY_ENDS_AT, endAt).remove(KEY_REASON).apply()
                handler.removeCallbacks(endRunnable)
                handler.postDelayed(endRunnable, (endAt - System.currentTimeMillis()).coerceAtLeast(0L))
                if (engine == null) startEngine()
            }
            ACTION_STOP -> stopSharing("stopped")
            // Restarted by the system with no intent: a session is only
            // ever started by the user, so don't resume one on our own.
            else -> stopSharing("ended")
        }
        return START_NOT_STICKY
    }

    private fun goForeground(endAt: Long, server: String): Boolean {
        ensureChannel(this)
        val notification = buildNotification(endAt, server)
        return try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
            } else {
                startForeground(NOTIFICATION_ID, notification)
            }
            true
        } catch (e: Exception) {
            // ForegroundServiceStartNotAllowedException: started from the
            // background, or the day's dataSync time is used up.
            Log.w(TAG, "startForeground refused: ${e.javaClass.simpleName}")
            finish("not_allowed")
            false
        }
    }

    private fun buildNotification(endAt: Long, server: String): Notification {
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(this)
        }
        val until = DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(endAt))
        val stopIntent = PendingIntent.getService(
            this, 1,
            Intent(this, CompanionShareService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        )
        val open = packageManager.getLaunchIntentForPackage(packageName)?.let {
            PendingIntent.getActivity(this, 2, it, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        }
        val text = if (server.isNotEmpty()) "$server can browse this phone until $until" else "Until $until"
        builder
            .setSmallIcon(smallIcon())
            .setContentTitle("Sharing this phone's storage")
            .setContentText(text)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setShowWhen(true)
            .setWhen(endAt)
            .setUsesChronometer(true)
            .setCategory(Notification.CATEGORY_SERVICE)
            .addAction(Notification.Action.Builder(null, "Stop sharing", stopIntent).build())
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) builder.setChronometerCountDown(true)
        if (open != null) builder.setContentIntent(open)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            builder.setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE)
        }
        return builder.build()
    }

    /** A monochrome status-bar icon: the one the old background plugin shipped, else a system one. */
    private fun smallIcon(): Icon {
        val id = resources.getIdentifier("ic_bg_service_small", "drawable", packageName)
        return if (id != 0) Icon.createWithResource(this, id) else Icon.createWithResource(this, android.R.drawable.stat_notify_sync)
    }

    private fun startEngine() {
        try {
            engine = HeadlessDart.start(this, "shareServiceMain") { e ->
                MethodChannel(e.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/share_engine").setMethodCallHandler { call, result ->
                    when (call.method) {
                        // Dart asks to end the session (signed out, no server).
                        "stop" -> {
                            result.success(true)
                            handler.post { stopSharing(call.argument<String>("reason") ?: "stopped") }
                        }
                        "endsAt" -> result.success(endsAt)
                        else -> result.notImplemented()
                    }
                }
            }
        } catch (e: Exception) {
            Log.e(TAG, "Could not start the sharing engine", e)
            stopSharing("error")
        }
    }

    /** API 34: the system ended a time-limited foreground service. */
    override fun onTimeout(startId: Int) {
        stopSharing("timeout")
    }

    /** API 35+: the dataSync budget (6 h per 24 h) is used up. Must stop now. */
    override fun onTimeout(startId: Int, fgsType: Int) {
        stopSharing("timeout")
    }

    private fun stopSharing(reason: String) {
        finish(reason)
    }

    private fun finish(reason: String) {
        handler.removeCallbacks(endRunnable)
        prefs().edit().putString(KEY_REASON, reason).putLong(KEY_ENDS_AT, 0L).apply()
        running = false
        endsAt = 0L
        engine?.destroy()
        engine = null
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) {
                stopForeground(STOP_FOREGROUND_REMOVE)
            } else {
                @Suppress("DEPRECATION")
                stopForeground(true)
            }
        } catch (_: Exception) {
        }
        stopSelf()
    }

    override fun onDestroy() {
        handler.removeCallbacks(endRunnable)
        engine?.destroy()
        engine = null
        running = false
        endsAt = 0L
        super.onDestroy()
    }

    private fun prefs() = getSharedPreferences(PREFS, Context.MODE_PRIVATE)
}
