package com.fenyx.nivaroos_mobile

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Log
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

/**
 * The companion heartbeat (plan M-18): about every 15 minutes, when there
 * is a network, the phone tells its server it is still there (battery,
 * storage, app version). It replaces the old 30-second loop in an
 * always-on foreground service, which Android 15 no longer allows and which
 * cost battery all day.
 *
 * JobScheduler (part of Android, no library) runs it; it survives reboots
 * (persisted job) and Doze batches it with other work. Each run starts a
 * headless Flutter engine with `heartbeatMain`, which registers once and
 * says "done". A run is cut off after [RUN_LIMIT_MS] whatever happens.
 */
class HeartbeatJobService : JobService() {
    companion object {
        private const val TAG = "Heartbeat"
        const val JOB_ID = 42844
        private const val PERIOD_MS = 15 * 60 * 1000L
        private const val RUN_LIMIT_MS = 90 * 1000L

        fun schedule(context: Context) {
            val js = context.getSystemService(JobScheduler::class.java) ?: return
            // Re-scheduling an identical job would restart its period.
            if (js.getPendingJob(JOB_ID) != null) return
            val info = JobInfo.Builder(JOB_ID, ComponentName(context, HeartbeatJobService::class.java))
                .setPeriodic(PERIOD_MS)
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setPersisted(true)
                .build()
            try {
                js.schedule(info)
            } catch (e: Exception) {
                Log.w(TAG, "Could not schedule the heartbeat: ${e.message}")
            }
        }

        fun cancel(context: Context) {
            context.getSystemService(JobScheduler::class.java)?.cancel(JOB_ID)
        }
    }

    private val handler = Handler(Looper.getMainLooper())
    private var engine: FlutterEngine? = null
    private var params: JobParameters? = null
    private val limit = Runnable { finish() }

    override fun onStartJob(p: JobParameters): Boolean {
        // While a sharing session runs, its engine already reports in.
        if (CompanionShareService.running) return false
        params = p
        return try {
            engine = HeadlessDart.start(this, "heartbeatMain") { e ->
                MethodChannel(e.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/heartbeat").setMethodCallHandler { call, result ->
                    if (call.method == "done") {
                        result.success(true)
                        handler.post { finish() }
                    } else {
                        result.notImplemented()
                    }
                }
            }
            handler.postDelayed(limit, RUN_LIMIT_MS)
            true
        } catch (e: Exception) {
            Log.e(TAG, "Heartbeat engine failed to start", e)
            cleanUp()
            false
        }
    }

    override fun onStopJob(p: JobParameters): Boolean {
        cleanUp()
        // Periodic: the next run comes anyway.
        return false
    }

    private fun finish() {
        val p = params
        cleanUp()
        if (p != null) jobFinished(p, false)
    }

    private fun cleanUp() {
        handler.removeCallbacks(limit)
        engine?.destroy()
        engine = null
        params = null
    }
}
