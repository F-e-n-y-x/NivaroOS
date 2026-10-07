package com.fenyx.nivaroos_mobile

import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.Uri
import android.net.wifi.WifiManager
import android.os.BatteryManager
import android.os.Build
import android.app.NotificationManager
import android.os.Bundle
import android.os.PowerManager
import android.provider.OpenableColumns
import android.provider.Settings
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import java.io.ByteArrayOutputStream

class MainActivity : FlutterActivity() {
    private var multicastLock: WifiManager.MulticastLock? = null
    private var phoneBackup: PhoneBackupBridge? = null

    // "Download on server": a link shared to the app, a magnet: link or a
    // .torrent file ({name, data}) opened with it, until Dart takes it
    // (lib/services/share_intent.dart).
    private var shareChannel: MethodChannel? = null
    private var pendingShare: Any? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Only on a fresh start: a re-created activity would offer it again.
        if (savedInstanceState == null) pendingShare = incoming(intent)
        val wifi = applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
        multicastLock = wifi?.createMulticastLock("nivaroos-mdns-discovery")?.apply {
            setReferenceCounted(true)
            acquire()
        }
        removeLegacyNotificationChannel()
    }

    // Builds up to 1.2.x ran an always-on service through
    // flutter_background_service, whose notification channel
    // ("FOREGROUND_DEFAULT") would otherwise stay in the app's
    // notification settings forever.
    private fun removeLegacyNotificationChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        try {
            getSystemService(NotificationManager::class.java)?.deleteNotificationChannel("FOREGROUND_DEFAULT")
        } catch (_: Exception) {
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        incoming(intent)?.let {
            pendingShare = it
            shareChannel?.invokeMethod("shared", null)
        }
    }

    private fun incoming(i: Intent?): Any? = if (i?.action == Intent.ACTION_VIEW) viewed(i) else sharedText(i)

    // ACTION_VIEW (manifest filters): a magnet: link, or a .torrent file a
    // file manager or browser hands over as a content:// URI it granted us
    // for this one intent. Nothing else is opened, and at most 16 MB is read.
    private fun viewed(i: Intent): Any? {
        val uri = i.data ?: return null
        if (uri.scheme.equals("magnet", ignoreCase = true)) return uri.toString().take(64 * 1024)
        if (uri.scheme != "content") return null
        return try {
            val name = contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                if (c.moveToFirst()) c.getString(0) else null
            } ?: "file.torrent"
            val data = contentResolver.openInputStream(uri)?.use { input ->
                val out = ByteArrayOutputStream()
                val chunk = ByteArray(64 * 1024)
                while (true) {
                    val n = input.read(chunk)
                    if (n < 0) break
                    out.write(chunk, 0, n)
                    if (out.size() > 16 * 1024 * 1024) return null
                }
                out.toByteArray()
            } ?: return null
            mapOf("name" to name, "data" to data)
        } catch (e: Exception) {
            null
        }
    }

    // Plain text only (a URL from a browser's Share): no streams or URIs
    // are read, so nothing another app grants or names is touched.
    private fun sharedText(i: Intent?): String? {
        if (i?.action != Intent.ACTION_SEND || i.type?.startsWith("text/") != true) return null
        return i.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString()?.take(64 * 1024)?.takeIf { it.isNotBlank() }
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        shareChannel = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/share_intent").also { ch ->
            ch.setMethodCallHandler { call, result ->
                when (call.method) {
                    "take" -> {
                        result.success(pendingShare)
                        pendingShare = null
                    }
                    "shareText" -> {
                        val send = Intent(Intent.ACTION_SEND).setType("text/plain")
                            .putExtra(Intent.EXTRA_TEXT, call.argument<String>("text") ?: "")
                        try {
                            startActivity(Intent.createChooser(send, call.argument<String>("title")))
                            result.success(true)
                        } catch (e: Exception) {
                            result.success(false)
                        }
                    }
                    else -> result.notImplemented()
                }
            }
        }

        // "Back up this phone" (PhoneBackupBridge, PhoneBackupJobService,
        // lib/phone_backup).
        phoneBackup = PhoneBackupBridge(applicationContext, this).also {
            MethodChannel(flutterEngine.dartExecutor.binaryMessenger, PhoneBackupBridge.CHANNEL).setMethodCallHandler(it)
        }

        // Device Info channel
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/device_info").setMethodCallHandler { call, result ->
            if (call.method == "getBatteryLevel") {
                result.success(getBatteryLevel())
            } else if (call.method == "getHardwareId") {
                try {
                    val androidId = Settings.Secure.getString(contentResolver, Settings.Secure.ANDROID_ID) ?: ""
                    result.success(androidId)
                } catch (e: Exception) {
                    result.success("")
                }
            } else {
                result.notImplemented()
            }
        }

        // Storage sharing sessions and the heartbeat job (see
        // CompanionShareService, HeartbeatJobService and
        // lib/services/background_service.dart).
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/companion_share").setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "start" -> {
                        val endAt = (call.argument<Number>("endAt") ?: 0).toLong()
                        CompanionShareService.start(this, endAt, call.argument<String>("server") ?: "")
                        result.success(true)
                    }
                    "stop" -> {
                        CompanionShareService.stop(this)
                        result.success(true)
                    }
                    "status" -> {
                        val prefs = getSharedPreferences(CompanionShareService.PREFS, Context.MODE_PRIVATE)
                        result.success(mapOf(
                            "running" to CompanionShareService.running,
                            "endsAt" to CompanionShareService.endsAt,
                            "lastStopReason" to prefs.getString(CompanionShareService.KEY_REASON, null),
                        ))
                    }
                    "scheduleHeartbeat" -> {
                        HeartbeatJobService.schedule(this)
                        result.success(true)
                    }
                    "cancelHeartbeat" -> {
                        HeartbeatJobService.cancel(this)
                        result.success(true)
                    }
                    else -> result.notImplemented()
                }
            } catch (e: Exception) {
                result.error("SHARE_ERROR", e.message, null)
            }
        }

        // Self-update (see AppUpdateInstaller and lib/services/app_update_service.dart).
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/app_update").setMethodCallHandler { call, result ->
            try {
                when (call.method) {
                    "installedSigners" -> result.success(AppUpdateInstaller.installedSigners(this))
                    // Hashing a 60 MB APK takes a moment: off the main thread.
                    "sha256" -> {
                        val path = call.argument<String>("path") ?: ""
                        Thread {
                            val hash = try { AppUpdateInstaller.fileSha256(path) } catch (e: Exception) { null }
                            runOnUiThread { result.success(hash) }
                        }.start()
                    }
                    "apkInfo" -> result.success(AppUpdateInstaller.apkInfo(this, call.argument<String>("path") ?: ""))
                    "canRequestInstalls" -> result.success(AppUpdateInstaller.canRequestInstalls(this))
                    "openInstallSettings" -> {
                        AppUpdateInstaller.openInstallSettings(this)
                        result.success(true)
                    }
                    "install" -> {
                        val path = call.argument<String>("path") ?: ""
                        Thread {
                            val error = try {
                                AppUpdateInstaller.install(this, path)
                                null
                            } catch (e: Exception) {
                                e.message ?: e.javaClass.simpleName
                            }
                            runOnUiThread {
                                if (error == null) result.success(true) else result.error("INSTALL_ERROR", error, null)
                            }
                        }.start()
                    }
                    "lastInstallError" -> {
                        val prefs = getSharedPreferences("nivaroos_update", Context.MODE_PRIVATE)
                        val error = prefs.getString("last_install_error", null)
                        prefs.edit().remove("last_install_error").apply()
                        result.success(error)
                    }
                    else -> result.notImplemented()
                }
            } catch (e: Exception) {
                result.error("UPDATE_ERROR", e.message, null)
            }
        }

        // Battery settings a headless engine has no Activity to open: the
        // Doze exemption dialog and Samsung's separate battery manager.
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/background_service").setMethodCallHandler { call, result ->
            when (call.method) {
                "isIgnoringBatteryOptimizations" -> {
                    val pm = getSystemService(Context.POWER_SERVICE) as? PowerManager
                    val isIgnoring = pm?.isIgnoringBatteryOptimizations(packageName) ?: false
                    result.success(isIgnoring)
                }
                "requestIgnoreBatteryOptimizations" -> {
                    try {
                        val pm = getSystemService(Context.POWER_SERVICE) as? PowerManager
                        if (pm?.isIgnoringBatteryOptimizations(packageName) == false) {
                            val intent = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS).apply {
                                data = Uri.parse("package:$packageName")
                                flags = Intent.FLAG_ACTIVITY_NEW_TASK
                            }
                            startActivity(intent)
                            result.success(true)
                        } else {
                            result.success(true)
                        }
                    } catch (e: Exception) {
                        try {
                            val fallbackIntent = Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS).apply {
                                flags = Intent.FLAG_ACTIVITY_NEW_TASK
                            }
                            startActivity(fallbackIntent)
                            result.success(true)
                        } catch (e2: Exception) {
                            result.error("INTENT_ERROR", e2.message, null)
                        }
                    }
                }
                "openBatteryOptimizationSettings" -> {
                    try {
                        val intent = Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS).apply {
                            flags = Intent.FLAG_ACTIVITY_NEW_TASK
                        }
                        startActivity(intent)
                        result.success(true)
                    } catch (e: Exception) {
                        result.error("INTENT_ERROR", e.message, null)
                    }
                }
                "isSamsungDevice" -> {
                    result.success(Build.MANUFACTURER.equals("samsung", ignoreCase = true))
                }
                "openSamsungBatterySettings" -> {
                    // No public AOSP Intent action reaches Samsung's own
                    // "Sleeping apps" / "Deep sleeping apps" list directly -
                    // these are One UI-specific screens with no documented,
                    // stable component name across OS versions/models. This
                    // opens the closest reliable entry point: this app's own
                    // battery usage settings page, from which "Never sleeping
                    // apps" is one tap away on One UI (the UI text prompt in
                    // Settings screen tells the user that next step).
                    try {
                        val intent = Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS).apply {
                            data = Uri.parse("package:$packageName")
                            flags = Intent.FLAG_ACTIVITY_NEW_TASK
                        }
                        startActivity(intent)
                        result.success(true)
                    } catch (e: Exception) {
                        result.error("INTENT_ERROR", e.message, null)
                    }
                }
                else -> result.notImplemented()
            }
        }
    }

    // BatteryManager.BATTERY_PROPERTY_CAPACITY returns -1 on some
    // devices/OEM skins instead of throwing (confirmed live on a Redmi/
    // HyperOS tablet - it silently always returned -1, which the Dart side's
    // `level >= 0` check then rejected, falling back to its hardcoded 100%
    // default forever, regardless of real battery state). The sticky
    // ACTION_BATTERY_CHANGED broadcast's EXTRA_LEVEL/EXTRA_SCALE is the
    // older but universally-supported way every Android device implements,
    // since it's how the system's own battery icon gets its value - used as
    // the primary source now, with BATTERY_PROPERTY_CAPACITY only as a
    // fallback if that broadcast is ever unavailable.
    private fun getBatteryLevel(): Int {
        try {
            val filter = IntentFilter(Intent.ACTION_BATTERY_CHANGED)
            val batteryStatus = applicationContext.registerReceiver(null, filter)
            val level = batteryStatus?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
            val scale = batteryStatus?.getIntExtra(BatteryManager.EXTRA_SCALE, -1) ?: -1
            if (level >= 0 && scale > 0) {
                return (level * 100 / scale)
            }
        } catch (e: Exception) {
            // fall through to the BatteryManager property below
        }

        return try {
            val bm = getSystemService(Context.BATTERY_SERVICE) as? BatteryManager
            bm?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY) ?: -1
        } catch (e: Exception) {
            -1
        }
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        if (phoneBackup?.onPermissions(requestCode) == true) return
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        if (phoneBackup?.onActivityResult(requestCode, resultCode, data) == true) return
        super.onActivityResult(requestCode, resultCode, data)
    }

    override fun onDestroy() {
        multicastLock?.let { if (it.isHeld) it.release() }
        super.onDestroy()
    }
}
