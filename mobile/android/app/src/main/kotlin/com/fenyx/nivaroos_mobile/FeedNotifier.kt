package com.fenyx.nivaroos_mobile

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.util.Log
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/**
 * The server's notification feed on the phone (lib/services/notification_feed.dart
 * decides what to show; this only posts it). Channel com.fenyx.nivaroos/feed_notify
 * on the app's engine and on the heartbeat's and sharing session's headless
 * ones. Three Android channels - Downloads, Backups, System - so each can be
 * silenced in Android's settings too. A tap opens MainActivity with
 * [ACTION_OPEN] and only the server and the entry's id: Dart reads the entry
 * from the server again, so another app's intent can't make up one.
 */
object FeedNotifier {
    const val ACTION_OPEN = "com.fenyx.nivaroos.FEED_OPEN"
    private const val TAG = "FeedNotifier"
    private val channels = mapOf(
        "downloads" to Pair("Downloads", "Downloads on your server finished or failed"),
        "backups" to Pair("Backups", "Backup & Sync runs that failed or need you"),
        "system" to Pair("System", "Apps, drives and other server news"),
    )

    fun register(engine: FlutterEngine, context: Context) {
        val app = context.applicationContext
        MethodChannel(engine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/feed_notify").setMethodCallHandler { call, result ->
            when (call.method) {
                "show" -> {
                    show(
                        app,
                        call.argument<String>("channel") ?: "system",
                        call.argument<String>("server") ?: "",
                        (call.argument<Number>("id") ?: 0).toLong(),
                        call.argument<String>("title") ?: "",
                        call.argument<String>("text") ?: "",
                    )
                    result.success(true)
                }
                "cancel" -> {
                    nm(app)?.cancel(call.argument<String>("server") ?: "", ((call.argument<Number>("id") ?: 0).toLong() and 0x7fffffff).toInt())
                    result.success(true)
                }
                else -> result.notImplemented()
            }
        }
    }

    private fun nm(context: Context) = context.getSystemService(NotificationManager::class.java)

    private fun ensureChannel(context: Context, id: String) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val nm = nm(context) ?: return
        if (nm.getNotificationChannel(id) != null) return
        val (name, about) = channels[id] ?: return
        nm.createNotificationChannel(NotificationChannel(id, name, NotificationManager.IMPORTANCE_DEFAULT).apply { description = about })
    }

    private fun show(context: Context, channel: String, server: String, id: Long, title: String, text: String) {
        val ch = if (channels.containsKey(channel)) channel else "system"
        ensureChannel(context, ch)
        // Tag = server, id = the entry's id: the same entry posted twice
        // replaces itself instead of showing twice.
        val nid = (id and 0x7fffffff).toInt()
        val open = Intent(context, MainActivity::class.java).setAction(ACTION_OPEN)
            .putExtra("server", server).putExtra("id", id)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        // Intent equality ignores extras: the request code keeps two servers'
        // entries with the same id from sharing (and rewriting) one intent.
        val pi = PendingIntent.getActivity(context, "$server/$id".hashCode(), open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) Notification.Builder(context, ch) else {
            @Suppress("DEPRECATION")
            Notification.Builder(context)
        }
        b.setSmallIcon(R.drawable.ic_stat_nivaroos)
            .setContentTitle(title)
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setAutoCancel(true)
            .setShowWhen(true)
            .setCategory(Notification.CATEGORY_STATUS)
            .setContentIntent(pi)
        try {
            nm(context)?.notify(server, nid, b.build())
        } catch (e: Exception) {
            // No POST_NOTIFICATIONS (Android 13+): nothing to show.
            Log.w(TAG, "notify failed: ${e.message}")
        }
    }
}
