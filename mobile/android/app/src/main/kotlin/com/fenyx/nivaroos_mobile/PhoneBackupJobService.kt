package com.fenyx.nivaroos_mobile

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.graphics.drawable.Icon
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PersistableBundle
import android.util.Log
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/**
 * "Back up this phone" in the background (lib/phone_backup). JobScheduler
 * (part of Android, persisted across reboots) runs it; each run starts a
 * headless Flutter engine with `phoneBackupMain`, which does the backup and
 * says "done" (with "more" when its time slice ran out).
 *
 * No foreground service: Android 14/15 cap dataSync services at 6 hours a
 * day and forbid starting them from the background. Instead
 *  - scheduled runs are ordinary jobs with the owner's conditions (unmetered
 *    network for Wi-Fi only, charging), cut into slices of [SLICE_MS]; a
 *    slice that runs out schedules the next at once (the backup resumes:
 *    same session, tus offsets on the server);
 *  - "Back up now" is a user-initiated data transfer job on Android 14+
 *    (no time limit, its own notification), an expedited job on 12-13,
 *    and an ordinary one before.
 * One notification (id [NOTIFICATION_ID]) shows the progress throughout.
 */
class PhoneBackupJobService : JobService() {
    companion object {
        private const val TAG = "PhoneBackup"
        const val NEXT_JOB_ID = 42850
        const val RUN_JOB_ID = 42851
        const val NOTIFICATION_ID = 42852
        const val CHANNEL_ID = "phone_backup"
        private const val PREFS = "nivaroos_phone_backup"
        private const val SLICE_MS = 8 * 60 * 1000L

        @Volatile
        var running = false
            private set

        fun ensureChannel(context: Context) {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
            val nm = context.getSystemService(NotificationManager::class.java) ?: return
            if (nm.getNotificationChannel(CHANNEL_ID) != null) return
            val channel = NotificationChannel(CHANNEL_ID, "Phone backup", NotificationManager.IMPORTANCE_LOW)
            channel.description = "Progress of backing up this phone to your NivaroOS server"
            channel.setShowBadge(false)
            nm.createNotificationChannel(channel)
        }

        fun notification(context: Context, title: String, text: String, done: Int, total: Int, ongoing: Boolean): Notification {
            ensureChannel(context)
            val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) Notification.Builder(context, CHANNEL_ID) else {
                @Suppress("DEPRECATION")
                Notification.Builder(context)
            }
            val open = context.packageManager.getLaunchIntentForPackage(context.packageName)?.let {
                PendingIntent.getActivity(context, 7, it, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
            }
            b.setSmallIcon(Icon.createWithResource(context, R.drawable.ic_stat_nivaroos))
                .setContentTitle(title)
                .setContentText(text)
                .setOngoing(ongoing)
                .setOnlyAlertOnce(true)
                .setShowWhen(false)
                .setCategory(if (ongoing) Notification.CATEGORY_PROGRESS else Notification.CATEGORY_STATUS)
            if (ongoing) b.setProgress(if (total > 0) total else 0, if (total > 0) done.coerceAtMost(total) else 0, total <= 0)
            if (open != null) b.setContentIntent(open)
            return b.build()
        }

        fun showProgress(context: Context, title: String, text: String, done: Int, total: Int, ongoing: Boolean) {
            try {
                context.getSystemService(NotificationManager::class.java)?.notify(NOTIFICATION_ID, notification(context, title, text, done, total, ongoing))
            } catch (e: Exception) {
                Log.w(TAG, "notify failed: ${e.message}")
            }
        }

        fun endProgress(context: Context, title: String?, text: String?) {
            val nm = context.getSystemService(NotificationManager::class.java) ?: return
            if (title.isNullOrEmpty()) {
                nm.cancel(NOTIFICATION_ID)
            } else {
                try {
                    nm.notify(NOTIFICATION_ID, notification(context, title, text ?: "", 0, 0, false))
                } catch (_: Exception) {
                }
            }
        }

        private fun builder(context: Context, id: Int) = JobInfo.Builder(id, ComponentName(context, PhoneBackupJobService::class.java))

        private fun extras(reason: String) = PersistableBundle().apply { putString("reason", reason) }

        /** The next scheduled run, [delayMs] from now, with the owner's conditions. */
        fun schedule(context: Context, delayMs: Long, wifiOnly: Boolean, charging: Boolean) {
            val js = context.getSystemService(JobScheduler::class.java) ?: return
            context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean("wifi_only", wifiOnly).putBoolean("charging", charging).apply()
            val info = builder(context, NEXT_JOB_ID)
                .setMinimumLatency(delayMs.coerceAtLeast(0L))
                .setRequiredNetworkType(if (wifiOnly) JobInfo.NETWORK_TYPE_UNMETERED else JobInfo.NETWORK_TYPE_ANY)
                .setRequiresCharging(charging)
                .setPersisted(true)
                .setExtras(extras("schedule"))
                .build()
            try {
                js.schedule(info)
            } catch (e: Exception) {
                Log.w(TAG, "Could not schedule the backup: ${e.message}")
            }
        }

        fun cancelSchedule(context: Context) {
            context.getSystemService(JobScheduler::class.java)?.cancel(NEXT_JOB_ID)
        }

        /**
         * Back up now, from the app (it is visible, as a user-initiated job
         * requires). [anyNetwork] false keeps it to unmetered networks.
         */
        fun runNow(context: Context, anyNetwork: Boolean): Boolean {
            if (running) return true
            val js = context.getSystemService(JobScheduler::class.java) ?: return false
            val net = if (anyNetwork) JobInfo.NETWORK_TYPE_ANY else JobInfo.NETWORK_TYPE_UNMETERED
            val b = builder(context, RUN_JOB_ID).setRequiredNetworkType(net).setExtras(extras("manual"))
            if (Build.VERSION.SDK_INT >= 34) {
                try {
                    b.setUserInitiated(true)
                    b.setEstimatedNetworkBytes(JobInfo.NETWORK_BYTES_UNKNOWN.toLong(), JobInfo.NETWORK_BYTES_UNKNOWN.toLong())
                    if (js.schedule(b.build()) == JobScheduler.RESULT_SUCCESS) return true
                } catch (e: Exception) {
                    Log.w(TAG, "User-initiated job refused: ${e.message}")
                }
            }
            val plain = builder(context, RUN_JOB_ID).setRequiredNetworkType(net).setExtras(extras("manual"))
            if (Build.VERSION.SDK_INT >= 31) {
                try {
                    plain.setExpedited(true)
                    if (js.schedule(plain.build()) == JobScheduler.RESULT_SUCCESS) return true
                } catch (e: Exception) {
                    Log.w(TAG, "Expedited job refused: ${e.message}")
                }
                plain.setExpedited(false)
            }
            return try {
                js.schedule(plain.build()) == JobScheduler.RESULT_SUCCESS
            } catch (e: Exception) {
                false
            }
        }

        /** The next slice of a run whose time ran out, as soon as its conditions hold. */
        fun scheduleContinuation(context: Context, anyNetwork: Boolean) {
            val js = context.getSystemService(JobScheduler::class.java) ?: return
            val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            val net = if (anyNetwork || !prefs.getBoolean("wifi_only", true)) JobInfo.NETWORK_TYPE_ANY else JobInfo.NETWORK_TYPE_UNMETERED
            val info = builder(context, RUN_JOB_ID)
                .setRequiredNetworkType(net)
                .setPersisted(true)
                .setExtras(extras("continue"))
                .build()
            try {
                js.schedule(info)
            } catch (e: Exception) {
                Log.w(TAG, "Could not schedule the next slice: ${e.message}")
            }
        }
    }

    private val handler = Handler(Looper.getMainLooper())
    private var engine: FlutterEngine? = null
    private var params: JobParameters? = null
    private var userInitiated = false
    private val hardLimit = Runnable {
        // The Dart side stops itself before its budget; this is the net.
        val p = params
        cleanUp()
        scheduleContinuation(this, userInitiated)
        if (p != null) jobFinished(p, false)
    }

    override fun onStartJob(p: JobParameters): Boolean {
        if (running) return false
        running = true
        params = p
        val reason = p.extras.getString("reason") ?: "schedule"
        userInitiated = Build.VERSION.SDK_INT >= 34 && p.isUserInitiatedJob
        if (userInitiated && Build.VERSION.SDK_INT >= 34) {
            try {
                setNotification(p, NOTIFICATION_ID, notification(this, "Backing up this phone", "Starting", 0, 0, true), JobService.JOB_END_NOTIFICATION_POLICY_DETACH)
            } catch (e: Exception) {
                Log.w(TAG, "setNotification failed: ${e.message}")
            }
        }
        val budget = if (userInitiated) 0L else SLICE_MS
        return try {
            engine = HeadlessDart.start(this, "phoneBackupMain") { e ->
                val messenger = e.dartExecutor.binaryMessenger
                MethodChannel(messenger, PhoneBackupBridge.CHANNEL).setMethodCallHandler(PhoneBackupBridge(applicationContext, null))
                MethodChannel(messenger, "com.fenyx.nivaroos/phone_backup_job").setMethodCallHandler { call, result ->
                    when (call.method) {
                        "start" -> result.success(mapOf("reason" to reason, "budgetMs" to budget, "userInitiated" to userInitiated))
                        "done" -> {
                            val more = call.argument<Boolean>("more") ?: false
                            result.success(true)
                            handler.post { done(more) }
                        }
                        else -> result.notImplemented()
                    }
                }
            }
            if (budget > 0) handler.postDelayed(hardLimit, budget + 90_000L)
            true
        } catch (e: Exception) {
            Log.e(TAG, "Backup engine failed to start", e)
            cleanUp()
            false
        }
    }

    private fun done(more: Boolean) {
        val p = params
        val ui = userInitiated
        cleanUp()
        if (more) scheduleContinuation(this, ui)
        if (p != null) jobFinished(p, false)
    }

    override fun onStopJob(p: JobParameters): Boolean {
        // Constraints lost (Wi-Fi gone, unplugged) or the system's limit:
        // everything confirmed is kept on the server; run again later.
        cleanUp()
        endProgress(this, "Phone backup paused", "It goes on when Wi-Fi, charging or the system allow")
        return true
    }

    private fun cleanUp() {
        handler.removeCallbacks(hardLimit)
        engine?.destroy()
        engine = null
        params = null
        running = false
    }
}
