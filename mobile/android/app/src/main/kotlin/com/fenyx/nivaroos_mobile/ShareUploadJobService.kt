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
import android.content.ClipData
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.drawable.Icon
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.os.PersistableBundle
import android.provider.OpenableColumns
import android.util.Log
import android.util.Size
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.io.FileInputStream
import java.io.FileNotFoundException
import java.io.InputStream
import java.util.concurrent.Executors

/**
 * "Upload to NivaroOS": files another app shared (MainActivity) uploaded in
 * the background by lib/services/share_upload.dart, one job per batch.
 *
 * Like "Back up now" (PhoneBackupJobService): a user-initiated data
 * transfer job on Android 14+ (no time limit, its own notification), an
 * expedited job on 12-13, an ordinary one before. The shared content://
 * URIs travel with the job as ClipData, so the job holds the read grant
 * after the app's screen is gone. Losing the network stops the job; it is
 * rescheduled and resumes (the server keeps the chunks it has).
 */
class ShareUploadJobService : JobService() {
    companion object {
        private const val TAG = "ShareUpload"
        private const val JOB_BASE = 42900
        private const val SLOTS = 50
        const val CHANNEL_ID = "share_upload"
        const val JOB_CHANNEL = "com.fenyx.nivaroos/share_upload_job"
        const val ACTION_OPEN = "com.fenyx.nivaroos.OPEN_UPLOAD"
        const val ACTION_CANCEL = "com.fenyx.nivaroos.CANCEL_UPLOAD"

        @Volatile
        private var instance: ShareUploadJobService? = null

        /** Schedules batch [batch] (its files in [uris]); false when Android refused. */
        fun start(context: Context, batch: String, uris: List<String>, bytes: Long): Boolean {
            val js = context.getSystemService(JobScheduler::class.java) ?: return false
            val id = slotFor(js, batch) ?: return false
            val clip = clipOf(uris)
            fun builder() = JobInfo.Builder(id, ComponentName(context, ShareUploadJobService::class.java))
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setExtras(PersistableBundle().apply { putString("batch", batch) })
                .apply { if (clip != null) setClipData(clip, Intent.FLAG_GRANT_READ_URI_PERMISSION) }
            if (Build.VERSION.SDK_INT >= 34) {
                try {
                    val b = builder().setUserInitiated(true).setEstimatedNetworkBytes(0, bytes.coerceAtLeast(0))
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

        private fun clipOf(uris: List<String>): ClipData? {
            if (uris.isEmpty()) return null
            val clip = ClipData.newRawUri("files", Uri.parse(uris[0]))
            for (u in uris.drop(1)) clip.addItem(ClipData.Item(Uri.parse(u)))
            return clip
        }

        /** The batch's own job id when it has one (a retry), else a free one. */
        private fun slotFor(js: JobScheduler, batch: String): Int? {
            val pending = js.allPendingJobs.filter { it.id in JOB_BASE until JOB_BASE + SLOTS }
            pending.firstOrNull { it.extras.getString("batch") == batch }?.let { return it.id }
            val used = pending.map { it.id }.toSet()
            return (JOB_BASE until JOB_BASE + SLOTS).firstOrNull { it !in used }
        }

        private fun ensureChannel(context: Context) {
            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
            val nm = context.getSystemService(NotificationManager::class.java) ?: return
            if (nm.getNotificationChannel(CHANNEL_ID) != null) return
            val channel = NotificationChannel(CHANNEL_ID, "Uploads", NotificationManager.IMPORTANCE_LOW)
            channel.description = "Files you share to NivaroOS, on their way to your server"
            channel.setShowBadge(false)
            nm.createNotificationChannel(channel)
        }

        /** Opens the app on the batch: its folder in Files, or its errors. */
        private fun openIntent(context: Context, id: Int, batch: String, folder: String?): PendingIntent {
            val i = Intent(context, MainActivity::class.java).setAction(ACTION_OPEN)
                .putExtra("batch", batch)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            if (folder != null) i.putExtra("folder", folder)
            return PendingIntent.getActivity(context, id, i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        }

        fun notification(context: Context, id: Int, batch: String, title: String, text: String, permille: Int, ongoing: Boolean, folder: String? = null): Notification {
            ensureChannel(context)
            val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) Notification.Builder(context, CHANNEL_ID) else {
                @Suppress("DEPRECATION")
                Notification.Builder(context)
            }
            val icon = Icon.createWithResource(context, R.drawable.ic_stat_nivaroos)
            b.setSmallIcon(icon)
                .setContentTitle(title)
                .setContentText(text)
                .setOngoing(ongoing)
                .setOnlyAlertOnce(true)
                .setShowWhen(!ongoing)
                .setAutoCancel(!ongoing)
                .setCategory(if (ongoing) Notification.CATEGORY_PROGRESS else Notification.CATEGORY_STATUS)
                .setContentIntent(openIntent(context, id, batch, if (ongoing) null else folder))
            if (ongoing) {
                b.setProgress(1000, permille.coerceIn(0, 1000), permille < 0)
                val cancel = Intent(context, CancelReceiver::class.java).setAction(ACTION_CANCEL).putExtra("batch", batch).putExtra("id", id)
                val pi = PendingIntent.getBroadcast(context, id, cancel, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
                b.addAction(Notification.Action.Builder(icon, "Cancel", pi).build())
            } else if (folder != null) {
                b.addAction(Notification.Action.Builder(icon, "Open", openIntent(context, id, batch, folder)).build())
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

    private class Run(val params: JobParameters, val engine: FlutterEngine, val channel: MethodChannel, val reader: SharedFiles.Reader)

    private val handler = Handler(Looper.getMainLooper())
    private val pool = Executors.newSingleThreadExecutor()
    private val runs = HashMap<String, Run>()

    override fun onCreate() {
        super.onCreate()
        instance = this
    }

    override fun onDestroy() {
        if (instance == this) instance = null
        pool.shutdown()
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
                setNotification(p, id, notification(this, id, batch, "Uploading to your server", "Starting", -1, true), JobService.JOB_END_NOTIFICATION_POLICY_DETACH)
            } catch (e: Exception) {
                Log.w(TAG, "setNotification failed: ${e.message}")
            }
        }
        return try {
            val reader = SharedFiles.Reader(applicationContext)
            lateinit var channel: MethodChannel
            val engine = HeadlessDart.start(this, "shareUploadMain") { e ->
                channel = MethodChannel(e.dartExecutor.binaryMessenger, JOB_CHANNEL)
                channel.setMethodCallHandler { call, result ->
                    when (call.method) {
                        "start" -> result.success(mapOf("batch" to batch, "userInitiated" to userInitiated))
                        "read" -> {
                            val uri = call.argument<String>("uri") ?: ""
                            val offset = (call.argument<Number>("offset") ?: 0).toLong()
                            val length = (call.argument<Number>("length") ?: 0).toInt()
                            pool.execute {
                                try {
                                    val bytes = reader.read(uri, offset, length)
                                    handler.post { result.success(bytes) }
                                } catch (e: SecurityException) {
                                    handler.post { result.error("PERMISSION", e.message, null) }
                                } catch (e: Exception) {
                                    handler.post { result.error("READ", "${e.javaClass.simpleName}: ${e.message}", null) }
                                }
                            }
                        }
                        "progress" -> {
                            notify(this, id, notification(this, id, batch, call.argument<String>("title") ?: "", call.argument<String>("text") ?: "", (call.argument<Number>("permille") ?: -1).toInt(), true))
                            result.success(true)
                        }
                        "done" -> {
                            result.success(true)
                            val title = call.argument<String>("title")
                            val text = call.argument<String>("text") ?: ""
                            val folder = call.argument<String>("folder")
                            val reschedule = call.argument<Boolean>("reschedule") ?: false
                            handler.post { finish(batch, reschedule, title, text, folder) }
                        }
                        else -> result.notImplemented()
                    }
                }
            }
            runs[batch] = Run(p, engine, channel, reader)
            true
        } catch (e: Exception) {
            Log.e(TAG, "Upload engine failed to start", e)
            false
        }
    }

    private fun finish(batch: String, reschedule: Boolean, title: String?, text: String, folder: String?) {
        val r = runs.remove(batch) ?: return
        val id = r.params.jobId
        cleanUp(r)
        if (!title.isNullOrEmpty()) notify(this, id, notification(this, id, batch, title, text, 0, reschedule, folder))
        jobFinished(r.params, reschedule)
    }

    override fun onStopJob(p: JobParameters): Boolean {
        // Network lost or the system's limit: run again later, picking up
        // where the server left off. Stopped by the user (Task Manager):
        // stays stopped.
        val batch = p.extras.getString("batch") ?: return false
        runs.remove(batch)?.let { cleanUp(it) }
        val byUser = Build.VERSION.SDK_INT >= 31 && p.stopReason == JobParameters.STOP_REASON_USER
        val title = if (byUser) "Upload stopped" else "Upload paused"
        val text = if (byUser) "Share the files again to upload them" else "It goes on when the network is back"
        notify(this, p.jobId, notification(this, p.jobId, batch, title, text, -1, !byUser))
        return !byUser
    }

    private fun cleanUp(r: Run) {
        r.engine.destroy()
        pool.execute { r.reader.close() }
    }
}

/** Reading what another app shared: names, sizes, thumbnails and bytes. */
object SharedFiles {
    /**
     * A shared item the app may read: a content:// URI of another app.
     * file:// and the app's own providers are refused, so a share can't
     * hand NivaroOS its own private files to upload.
     */
    fun allowed(ctx: Context, uri: Uri): Boolean {
        if (uri.scheme != "content") return false
        val authority = uri.authority ?: return false
        return try {
            ctx.packageManager.resolveContentProvider(authority, 0)?.packageName != ctx.packageName
        } catch (e: Exception) {
            true
        }
    }

    /** {uri, name, size, mime}; null when it can't be read. Off the main thread. */
    fun describe(ctx: Context, uri: Uri): Map<String, Any>? {
        if (!allowed(ctx, uri)) return null
        val cr = ctx.contentResolver
        var name: String? = null
        var size = -1L
        try {
            cr.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
                if (c.moveToFirst()) {
                    val n = c.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val s = c.getColumnIndex(OpenableColumns.SIZE)
                    if (n >= 0 && !c.isNull(n)) name = c.getString(n)
                    if (s >= 0 && !c.isNull(s)) size = c.getLong(s)
                }
            }
            if (size < 0) cr.openAssetFileDescriptor(uri, "r")?.use { size = it.length }
            // Some providers only stream: count it.
            if (size < 0) cr.openInputStream(uri)?.use { s ->
                val buf = ByteArray(1 shl 16)
                var n = 0L
                while (true) {
                    val r = s.read(buf)
                    if (r < 0) break
                    n += r
                }
                size = n
            }
        } catch (e: Exception) {
            Log.w("ShareUpload", "Can't read a shared file: ${e.javaClass.simpleName}")
            return null
        }
        if (size < 0) return null
        val mime = try { cr.getType(uri) } catch (e: Exception) { null } ?: ""
        return mapOf("uri" to uri.toString(), "name" to (name ?: uri.lastPathSegment ?: "file"), "size" to size, "mime" to mime)
    }

    /** A JPEG thumbnail of a picture or video, Android 10+; null otherwise. */
    fun thumbnail(ctx: Context, uri: Uri, px: Int): ByteArray? {
        if (Build.VERSION.SDK_INT < 29 || !allowed(ctx, uri)) return null
        return try {
            val bmp = ctx.contentResolver.loadThumbnail(uri, Size(px, px), null)
            ByteArrayOutputStream().use { out ->
                bmp.compress(Bitmap.CompressFormat.JPEG, 80, out)
                out.toByteArray()
            }
        } catch (e: Exception) {
            null
        }
    }

    /**
     * Reads a shared file in order, chunk after chunk, from one open
     * stream: providers that only stream (no seeking) aren't read again
     * from the start for every chunk.
     */
    class Reader(private val ctx: Context) : Closeable {
        private var uri: String? = null
        private var pfd: ParcelFileDescriptor? = null
        private var stream: InputStream? = null
        private var pos = 0L

        @Synchronized
        fun read(u: String, offset: Long, length: Int): ByteArray {
            if (u != uri || offset != pos || stream == null) open(u, offset)
            val s = stream!!
            val buf = ByteArray(length)
            var got = 0
            while (got < length) {
                val n = s.read(buf, got, length - got)
                if (n < 0) break
                got += n
            }
            pos += got
            return if (got == length) buf else buf.copyOf(got)
        }

        private fun open(u: String, offset: Long) {
            close()
            val parsed = Uri.parse(u)
            if (!allowed(ctx, parsed)) throw SecurityException("not a shared file")
            try {
                val fd = ctx.contentResolver.openFileDescriptor(parsed, "r")
                if (fd != null) {
                    val s = FileInputStream(fd.fileDescriptor)
                    try {
                        s.channel.position(offset)
                        pfd = fd
                        stream = s
                        uri = u
                        pos = offset
                        return
                    } catch (e: Exception) {
                        // A pipe: no seeking.
                        s.close()
                        fd.close()
                    }
                }
            } catch (e: SecurityException) {
                throw e
            } catch (e: Exception) {
                // Some providers only stream.
            }
            val s = ctx.contentResolver.openInputStream(parsed) ?: throw FileNotFoundException(u)
            var skip = offset
            val buf = ByteArray(1 shl 16)
            while (skip > 0) {
                val n = s.read(buf, 0, minOf(buf.size.toLong(), skip).toInt())
                if (n < 0) break
                skip -= n
            }
            stream = s
            uri = u
            pos = offset - skip
        }

        @Synchronized
        override fun close() {
            try { stream?.close() } catch (_: Exception) {}
            try { pfd?.close() } catch (_: Exception) {}
            stream = null
            pfd = null
            uri = null
        }
    }
}
