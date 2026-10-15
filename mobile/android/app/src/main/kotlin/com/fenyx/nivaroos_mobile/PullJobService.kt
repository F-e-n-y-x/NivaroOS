package com.fenyx.nivaroos_mobile

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.BroadcastReceiver
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.PersistableBundle
import android.util.Log
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/**
 * A large pull from the server or another phone to this phone (Files'
 * "Download to this phone"), run by lib/services/pull_job.dart, one job per
 * batch. The same kind of job as "Upload to NivaroOS"
 * (ShareUploadJobService): user-initiated data transfer on Android 14+ (no
 * time limit, its own notification), expedited on 12-13, ordinary before.
 * Losing the network stops it; it is rescheduled and resumes each file from
 * the byte it reached (the part files stay on disk).
 */
class PullJobService : JobService() {
    companion object {
        private const val TAG = "Pull"
        private const val JOB_BASE = 43000
        private const val SLOTS = 20
        const val CHANNEL_ID = "pull"
        const val JOB_CHANNEL = "com.fenyx.nivaroos/pull_job"
        const val ACTION_CANCEL = "com.fenyx.nivaroos.CANCEL_PULL"

        @Volatile
        private var instance: PullJobService? = null

        /** Schedules batch [batch]; false when Android refused (the app then pulls in the foreground). */
        fun start(context: Context, batch: String, bytes: Long): Boolean {
            val js = context.getSystemService(JobScheduler::class.java) ?: return false
            val pending = js.allPendingJobs.filter { it.id in JOB_BASE until JOB_BASE + SLOTS }
            val id = pending.firstOrNull { it.extras.getString("batch") == batch }?.id
                ?: (JOB_BASE until JOB_BASE + SLOTS).firstOrNull { slot -> pending.none { it.id == slot } }
                ?: return false
            fun builder() = JobInfo.Builder(id, ComponentName(context, PullJobService::class.java))
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setExtras(PersistableBundle().apply { putString("batch", batch) })
            if (Build.VERSION.SDK_INT >= 34) {
                try {
                    val b = builder().setUserInitiated(true).setEstimatedNetworkBytes(bytes.coerceAtLeast(0), 0)
                    if (js.schedule(b.build()) == JobScheduler.RESULT_SUCCESS) return true
                } catch (e: Exception) {
                    Log.w(TAG, "User-initiated job refused: ${e.message}")
                }
            }
            if (Build.VERSION.SDK_INT >= 31) {
                try {
                    if (js.schedule(builder().setExpedited(true).build()) == JobScheduler.RESULT_SUCCESS) return true
                } catch (e: Exception) {
                    Log.w(TAG, "Expedited job refused: ${e.message}")
                }
            }
            return try {
                js.schedule(builder().build()) == JobScheduler.RESULT_SUCCESS
            } catch (e: Exception) {
                false
            }
        }

        private fun ensureChannel(context: Context) {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
            val nm = context.getSystemService(NotificationManager::class.java) ?: return
            if (nm.getNotificationChannel(CHANNEL_ID) != null) return
            val channel = NotificationChannel(CHANNEL_ID, "Downloads", NotificationManager.IMPORTANCE_LOW)
            channel.description = "Files on their way from your server or your other phones to this one"
            channel.setShowBadge(false)
            nm.createNotificationChannel(channel)
        }

        fun notification(context: Context, id: Int, batch: String, title: String, text: String, permille: Int, ongoing: Boolean): Notification {
            ensureChannel(context)
            val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) Notification.Builder(context, CHANNEL_ID) else {
                @Suppress("DEPRECATION")
                Notification.Builder(context)
            }
            val icon = Icon.createWithResource(context, R.drawable.ic_stat_nivaroos)
            val open = Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            b.setSmallIcon(icon)
                .setContentTitle(title)
                .setContentText(text)
                .setOngoing(ongoing)
                .setOnlyAlertOnce(true)
                .setShowWhen(!ongoing)
                .setAutoCancel(!ongoing)
                .setCategory(if (ongoing) Notification.CATEGORY_PROGRESS else Notification.CATEGORY_STATUS)
                .setContentIntent(PendingIntent.getActivity(context, id, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            if (ongoing) {
                b.setProgress(1000, permille.coerceIn(0, 1000), permille < 0)
                val cancel = Intent(context, CancelReceiver::class.java).setAction(ACTION_CANCEL).putExtra("batch", batch).putExtra("id", id)
                val pi = PendingIntent.getBroadcast(context, id, cancel, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
                b.addAction(Notification.Action.Builder(icon, "Cancel", pi).build())
            }
            return b.build()
        }

        private fun notify(context: Context, id: Int, n: Notification) {
            try {
                context.getSystemService(NotificationManager::class.java)?.notify(id, n)
            } catch (e: Exception) {
                Log.w(TAG, "notify failed: ${e.message}")
            }
        }
    }

    /** The Cancel button: the running batch stops and says so. */
    class CancelReceiver : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            val batch = intent.getStringExtra("batch") ?: return
            val id = intent.getIntExtra("id", 0)
            if (instance?.cancel(batch) == true) return
            context.getSystemService(JobScheduler::class.java)?.cancel(id)
            context.getSystemService(NotificationManager::class.java)?.cancel(id)
        }
    }

    private class Run(val params: JobParameters, val engine: FlutterEngine, val channel: MethodChannel)

    private val handler = Handler(Looper.getMainLooper())
    private val runs = HashMap<String, Run>()

    override fun onCreate() {
        super.onCreate()
        instance = this
    }

    override fun onDestroy() {
        if (instance == this) instance = null
        super.onDestroy()
    }

    fun cancel(batch: String): Boolean {
        val r = runs[batch] ?: return false
        r.channel.invokeMethod("cancel", null)
        return true
    }

    override fun onStartJob(p: JobParameters): Boolean {
        val batch = p.extras.getString("batch") ?: return false
        if (runs.containsKey(batch)) return false
        val id = p.jobId
        val userInitiated = Build.VERSION.SDK_INT >= 34 && p.isUserInitiatedJob
        if (userInitiated && Build.VERSION.SDK_INT >= 34) {
            try {
                setNotification(p, id, notification(this, id, batch, "Downloading to this phone", "Starting", -1, true), JobService.JOB_END_NOTIFICATION_POLICY_DETACH)
            } catch (e: Exception) {
                Log.w(TAG, "setNotification failed: ${e.message}")
            }
        }
        return try {
            lateinit var channel: MethodChannel
            val engine = HeadlessDart.start(this, "pullMain") { e ->
                channel = MethodChannel(e.dartExecutor.binaryMessenger, JOB_CHANNEL)
                channel.setMethodCallHandler { call, result ->
                    when (call.method) {
                        "start" -> result.success(mapOf("batch" to batch, "userInitiated" to userInitiated))
                        "progress" -> {
                            notify(this, id, notification(this, id, batch, call.argument<String>("title") ?: "", call.argument<String>("text") ?: "", (call.argument<Number>("permille") ?: -1).toInt(), true))
                            result.success(true)
                        }
                        "done" -> {
                            result.success(true)
                            val title = call.argument<String>("title")
                            val text = call.argument<String>("text") ?: ""
                            val reschedule = call.argument<Boolean>("reschedule") ?: false
                            handler.post { finish(batch, reschedule, title, text) }
                        }
                        else -> result.notImplemented()
                    }
                }
            }
            runs[batch] = Run(p, engine, channel)
            true
        } catch (e: Exception) {
            Log.e(TAG, "Download engine failed to start", e)
            false
        }
    }

    private fun finish(batch: String, reschedule: Boolean, title: String?, text: String) {
        val r = runs.remove(batch) ?: return
        val id = r.params.jobId
        r.engine.destroy()
        if (!title.isNullOrEmpty()) notify(this, id, notification(this, id, batch, title, text, 0, reschedule))
        jobFinished(r.params, reschedule)
    }

    override fun onStopJob(p: JobParameters): Boolean {
        // Network lost or the system's limit: run again later, resuming
        // each file. Stopped by the user (Task Manager): stays stopped.
        val batch = p.extras.getString("batch") ?: return false
        runs.remove(batch)?.engine?.destroy()
        val byUser = Build.VERSION.SDK_INT >= 31 && p.stopReason == JobParameters.STOP_REASON_USER
        val title = if (byUser) "Download stopped" else "Download paused"
        val text = if (byUser) "Download the files again from Files to finish" else "It goes on when the network is back"
        notify(this, p.jobId, notification(this, p.jobId, batch, title, text, -1, !byUser))
        return !byUser
    }
}
