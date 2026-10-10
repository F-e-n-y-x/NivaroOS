package com.fenyx.nivaroos_mobile

import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.graphics.Color
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import android.widget.RemoteViews
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.File

/**
 * Home-screen widgets and Quick Settings tiles. They draw text the Dart side
 * (lib/services/home_widgets.dart) pushes over [CHANNEL] from the heartbeat,
 * the app and the phone backup job, kept in [PREFS]. Nothing here polls or
 * talks to the server.
 */
object HomeWidgets {
    const val CHANNEL = "com.fenyx.nivaroos/home_widgets"
    private const val PREFS = "nivaroos_widgets"
    private val OK = Color.parseColor("#2E9E5B")
    private val BAD = Color.parseColor("#D64545")
    private val IDLE = Color.parseColor("#8A8F98")

    fun prefs(context: Context): SharedPreferences = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun register(engine: FlutterEngine, context: Context) {
        val app = context.applicationContext
        MethodChannel(engine.dartExecutor.binaryMessenger, CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "update" -> {
                    val e = prefs(app).edit()
                    (call.arguments as? Map<*, *>)?.forEach { (k, v) ->
                        val key = k as? String ?: return@forEach
                        when (v) {
                            is String -> e.putString(key, v)
                            is Boolean -> e.putBoolean(key, v)
                            is Number -> e.putLong(key, v.toLong())
                            null -> e.remove(key)
                        }
                    }
                    e.apply()
                    redraw(app)
                    result.success(true)
                }
                // Signed out: the server's part goes; phone backup has its own link.
                "clear" -> {
                    val p = prefs(app)
                    val e = p.edit()
                    p.all.keys.filterNot { it.startsWith("backup") }.forEach { e.remove(it) }
                    e.apply()
                    redraw(app)
                    result.success(true)
                }
                else -> result.notImplemented()
            }
        }
    }

    /** Redraws every placed widget; the tiles read [PREFS] when shown. */
    fun redraw(context: Context) {
        val mgr = AppWidgetManager.getInstance(context)
        mgr.getAppWidgetIds(ComponentName(context, ServerStatusWidget::class.java)).forEach { mgr.updateAppWidget(it, server(context)) }
        mgr.getAppWidgetIds(ComponentName(context, TransfersWidget::class.java)).forEach { mgr.updateAppWidget(it, transfers(context)) }
    }

    fun openApp(context: Context): PendingIntent? =
        context.packageManager.getLaunchIntentForPackage(context.packageName)?.let {
            it.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            PendingIntent.getActivity(context, 9, it, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        }

    /** "Online", "Offline", the stale text once its time passed, or a sign-in hint. */
    fun status(p: SharedPreferences): String = when {
        !p.getBoolean("signed_in", false) -> "Sign in to NivaroOS"
        System.currentTimeMillis() > p.getLong("stale_at", 0) -> p.getString("stale", null) ?: "Not updated recently"
        else -> p.getString("status", null) ?: "Not updated yet"
    }

    private fun server(context: Context): RemoteViews {
        val p = prefs(context)
        val v = RemoteViews(context.packageName, R.layout.widget_server)
        val status = status(p)
        v.setTextViewText(R.id.widget_title, p.getString("server", null)?.takeIf { it.isNotEmpty() } ?: "NivaroOS")
        v.setTextViewText(R.id.widget_status, status)
        v.setTextColor(R.id.widget_dot, if (status == "Online") OK else if (status == "Offline") BAD else IDLE)
        v.setTextViewText(R.id.widget_cpu, p.getString("cpu", null) ?: "—")
        v.setTextViewText(R.id.widget_ram, p.getString("ram", null) ?: "—")
        v.setTextViewText(R.id.widget_disk, p.getString("disk", null) ?: "—")
        v.setTextViewText(R.id.widget_updated, p.getString("updated", null) ?: "")
        openApp(context)?.let { v.setOnClickPendingIntent(R.id.widget_root, it) }
        return v
    }

    private fun transfers(context: Context): RemoteViews {
        val p = prefs(context)
        val v = RemoteViews(context.packageName, R.layout.widget_transfers)
        val signedIn = p.getBoolean("signed_in", false)
        v.setTextViewText(R.id.widget_downloads, if (signedIn) p.getString("downloads", null) ?: "Downloads not checked yet" else "Sign in to NivaroOS")
        val backup = if (PhoneBackupJobService.running) "Backing up this phone" else p.getString("backup", null) ?: "Phone backup not set up"
        v.setTextViewText(R.id.widget_backup, backup)
        openApp(context)?.let { v.setOnClickPendingIntent(R.id.widget_root, it) }
        return v
    }

    /** From a tile: open the app, collapsing the shade (and unlocking first). */
    fun openFromTile(tile: TileService) {
        val go = Runnable {
            if (Build.VERSION.SDK_INT >= 34) {
                openApp(tile)?.let { tile.startActivityAndCollapse(it) }
            } else {
                tile.packageManager.getLaunchIntentForPackage(tile.packageName)?.let {
                    it.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                    @Suppress("DEPRECATION")
                    tile.startActivityAndCollapse(it)
                }
            }
        }
        if (tile.isLocked) tile.unlockAndRun(go) else go.run()
    }

    fun setTile(tile: Tile?, state: Int, subtitle: String) {
        tile ?: return
        tile.state = state
        if (Build.VERSION.SDK_INT >= 29) tile.subtitle = subtitle
        tile.updateTile()
    }
}

/** Server status: CPU, memory, storage, online or not, when. Updated on each push, and every 30 min for the stale check. */
class ServerStatusWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) = HomeWidgets.redraw(context)
}

/** Active downloads and this phone's backup. */
class TransfersWidget : AppWidgetProvider() {
    override fun onUpdate(context: Context, manager: AppWidgetManager, ids: IntArray) = HomeWidgets.redraw(context)
}

/** Quick Settings "Server status": shows the last reading; a tap opens the app. */
class ServerStatusTile : TileService() {
    override fun onStartListening() {
        val p = HomeWidgets.prefs(this)
        val status = HomeWidgets.status(p)
        val text = if (status == "Online") "CPU ${p.getString("cpu", null) ?: "—"} · RAM ${p.getString("ram", null) ?: "—"}" else status
        HomeWidgets.setTile(qsTile, if (status == "Online") Tile.STATE_ACTIVE else Tile.STATE_INACTIVE, text)
    }

    override fun onClick() = HomeWidgets.openFromTile(this)
}

/** Quick Settings "Back up this phone now": starts the phone backup once it is set up, else opens the app. */
class BackupNowTile : TileService() {
    override fun onStartListening() {
        val p = HomeWidgets.prefs(this)
        when {
            PhoneBackupJobService.running -> HomeWidgets.setTile(qsTile, Tile.STATE_ACTIVE, "Backing up")
            p.getBoolean("backup_ready", false) -> HomeWidgets.setTile(qsTile, Tile.STATE_INACTIVE, p.getString("backup", null) ?: "")
            else -> HomeWidgets.setTile(qsTile, Tile.STATE_INACTIVE, "Set up in the app")
        }
    }

    override fun onClick() {
        val p = HomeWidgets.prefs(this)
        if (!p.getBoolean("backup_ready", false)) return HomeWidgets.openFromTile(this)
        // As PhoneBackupService.backUpNow: a stale stop request would end the run at once.
        File(filesDir, "phone_backup/cancel").delete()
        val ok = PhoneBackupJobService.runNow(this, p.getBoolean("backup_any_network", false))
        HomeWidgets.setTile(qsTile, if (ok) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE, if (ok) "Starting" else "Couldn't start")
    }
}
