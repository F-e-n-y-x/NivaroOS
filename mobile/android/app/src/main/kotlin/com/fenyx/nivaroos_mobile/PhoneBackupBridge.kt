package com.fenyx.nivaroos_mobile

import android.Manifest
import android.app.Activity
import android.app.role.RoleManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.provider.Telephony
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.Executors

/**
 * The com.fenyx.nivaroos/phone_backup channel (lib/phone_backup/pb_platform.dart),
 * on the app's engine (with [activity]) and on the backup job's headless
 * engine (without: the methods that need a screen answer an error there).
 * Reads run on a small pool, never on the main thread.
 */
class PhoneBackupBridge(private val context: Context, private val activity: Activity?) : MethodChannel.MethodCallHandler {
    companion object {
        const val CHANNEL = "com.fenyx.nivaroos/phone_backup"
        const val REQ_PERMISSIONS = 4861
        const val REQ_TREE = 4862
        const val REQ_SMS_ROLE = 4863
        private val pool = Executors.newFixedThreadPool(3)

        /** Media permissions by Android version (READ_MEDIA_* from 13). */
        fun effective(name: String): String? = when (name) {
            Manifest.permission.READ_MEDIA_IMAGES, Manifest.permission.READ_MEDIA_VIDEO ->
                if (Build.VERSION.SDK_INT >= 33) name else Manifest.permission.READ_EXTERNAL_STORAGE
            Manifest.permission.ACCESS_MEDIA_LOCATION -> if (Build.VERSION.SDK_INT >= 29) name else null
            Manifest.permission.POST_NOTIFICATIONS -> if (Build.VERSION.SDK_INT >= 33) name else null
            else -> name
        }
    }

    private val main = Handler(Looper.getMainLooper())
    private var pendingPermissions: Pair<List<String>, MethodChannel.Result>? = null
    private var pendingTree: MethodChannel.Result? = null
    private var pendingRole: MethodChannel.Result? = null

    private fun background(result: MethodChannel.Result, work: () -> Any?) {
        pool.execute {
            try {
                val v = work()
                main.post { result.success(v) }
            } catch (e: SecurityException) {
                main.post { result.error("PERMISSION", "SecurityException: ${e.message}", null) }
            } catch (e: Exception) {
                main.post { result.error("FAILED", "${e.javaClass.simpleName}: ${e.message}", null) }
            }
        }
    }

    private fun granted(name: String): Boolean {
        val eff = effective(name) ?: return true
        return context.checkSelfPermission(eff) == PackageManager.PERMISSION_GRANTED
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        fun s(k: String) = call.argument<String>(k) ?: ""
        fun l(k: String) = (call.argument<Number>(k) ?: 0).toLong()
        fun i(k: String) = (call.argument<Number>(k) ?: 0).toInt()
        when (call.method) {
            "scanMedia" -> background(result) { PhoneBackupData.scanMedia(context) }
            "scanTree" -> background(result) { PhoneBackupData.scanTree(context, s("uri"), s("label")) }
            "sha256" -> background(result) { PhoneBackupData.sha256(context, s("source")) }
            "read" -> background(result) { PhoneBackupData.read(context, s("source"), l("offset"), i("length")) }
            "exportContacts" -> background(result) { PhoneBackupData.exportContacts(context, s("dir")) }
            "exportCalendars" -> background(result) { PhoneBackupData.exportCalendars(context, s("dir")) }
            "readSms" -> background(result) { PhoneBackupData.readSms(context, i("offset"), i("limit")) }
            "readMms" -> background(result) { PhoneBackupData.readMms(context, i("offset"), i("limit")) }
            "mmsParts" -> background(result) { PhoneBackupData.mmsParts(context, l("id"), i("maxBytes")) }
            "readCallLog" -> background(result) { PhoneBackupData.readCallLog(context, i("offset"), i("limit")) }
            "installedApps" -> background(result) { PhoneBackupData.installedApps(context) }
            "readSettings" -> background(result) { PhoneBackupData.readSettings(context) }
            "permissions" -> {
                val names = call.argument<List<String>>("names") ?: emptyList()
                result.success(names.associateWith { granted(it) })
            }
            "progress" -> {
                PhoneBackupJobService.showProgress(context, s("title"), s("text"), i("done"), i("total"), call.argument<Boolean>("ongoing") ?: true)
                result.success(true)
            }
            "endProgress" -> {
                PhoneBackupJobService.endProgress(context, call.argument<String>("title"), call.argument<String>("text"))
                result.success(true)
            }
            "schedule" -> {
                PhoneBackupJobService.schedule(context, l("delayMs"), call.argument<Boolean>("wifiOnly") ?: true, call.argument<Boolean>("charging") ?: false)
                result.success(true)
            }
            "cancelSchedule" -> {
                PhoneBackupJobService.cancelSchedule(context)
                result.success(true)
            }
            "isRunning" -> result.success(PhoneBackupJobService.running)
            "deviceInfo" -> result.success(mapOf(
                "model" to "${Build.MANUFACTURER} ${Build.MODEL}".trim(),
                "os" to "Android ${Build.VERSION.RELEASE}",
                "sdk" to Build.VERSION.SDK_INT.toString(),
            ))
            "saveToMedia" -> background(result) { PhoneBackupData.saveToMedia(context, s("tmp"), s("relativePath"), s("name"), l("takenAt"), l("mtime")) }
            "saveToDownloads" -> background(result) { PhoneBackupData.saveToDownloads(context, s("tmp"), s("subPath"), s("name")) }
            "mediaExists" -> background(result) { PhoneBackupData.mediaExists(context, s("relativePath"), s("name"), l("size")) }
            "isDefaultSmsApp" -> result.success(isDefaultSms())
            "restoreSms" -> {
                if (!isDefaultSms()) {
                    result.error("NOT_DEFAULT_SMS", "NivaroOS is not the default SMS app", null)
                } else {
                    background(result) { PhoneBackupData.restoreSms(context, s("path")) }
                }
            }
            "restoreCallLog" -> background(result) { PhoneBackupData.restoreCallLog(context, s("path")) }
            // The rest need a screen.
            else -> {
                val a = activity
                if (a == null) {
                    result.error("NO_ACTIVITY", "${call.method} needs the app on screen", null)
                    return
                }
                activityCall(a, call, result)
            }
        }
    }

    private fun isDefaultSms(): Boolean = try {
        Telephony.Sms.getDefaultSmsPackage(context) == context.packageName
    } catch (e: Exception) {
        false
    }

    private fun activityCall(a: Activity, call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "runNow" -> result.success(PhoneBackupJobService.runNow(a, call.argument<Boolean>("anyNetwork") ?: false))
            "requestPermissions" -> {
                val names = call.argument<List<String>>("names") ?: emptyList()
                val ask = names.mapNotNull { effective(it) }.distinct().filter { a.checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED }
                if (ask.isEmpty()) {
                    result.success(names.associateWith { granted(it) })
                    return
                }
                pendingPermissions?.second?.success(null)
                pendingPermissions = names to result
                a.requestPermissions(ask.toTypedArray(), REQ_PERMISSIONS)
            }
            "pickFolder" -> {
                pendingTree?.success(null)
                pendingTree = result
                try {
                    a.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT_TREE).addFlags(Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION or Intent.FLAG_GRANT_READ_URI_PERMISSION), REQ_TREE)
                } catch (e: Exception) {
                    pendingTree = null
                    result.error("NO_PICKER", e.message, null)
                }
            }
            "releaseFolder" -> {
                try {
                    a.contentResolver.releasePersistableUriPermission(Uri.parse(call.argument<String>("uri") ?: ""), Intent.FLAG_GRANT_READ_URI_PERMISSION)
                } catch (_: Exception) {
                }
                result.success(true)
            }
            "openAppSettings" -> {
                a.startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${a.packageName}")))
                result.success(true)
            }
            "openDefaultApps" -> {
                try {
                    a.startActivity(Intent(Settings.ACTION_MANAGE_DEFAULT_APPS_SETTINGS))
                } catch (e: Exception) {
                    a.startActivity(Intent(Settings.ACTION_SETTINGS))
                }
                result.success(true)
            }
            "requestSmsRole" -> {
                if (isDefaultSms()) {
                    result.success(true)
                    return
                }
                pendingRole?.success(false)
                pendingRole = result
                try {
                    val intent = if (Build.VERSION.SDK_INT >= 29) {
                        val rm = a.getSystemService(RoleManager::class.java)
                        if (rm == null || !rm.isRoleAvailable(RoleManager.ROLE_SMS)) throw IllegalStateException("No SMS role")
                        rm.createRequestRoleIntent(RoleManager.ROLE_SMS)
                    } else {
                        Intent(Telephony.Sms.Intents.ACTION_CHANGE_DEFAULT).putExtra(Telephony.Sms.Intents.EXTRA_PACKAGE_NAME, a.packageName)
                    }
                    a.startActivityForResult(intent, REQ_SMS_ROLE)
                } catch (e: Exception) {
                    pendingRole = null
                    result.error("NO_ROLE", e.message, null)
                }
            }
            else -> result.notImplemented()
        }
    }

    /** From MainActivity.onRequestPermissionsResult; true when it was ours. */
    fun onPermissions(requestCode: Int): Boolean {
        if (requestCode != REQ_PERMISSIONS) return false
        val p = pendingPermissions ?: return true
        pendingPermissions = null
        p.second.success(p.first.associateWith { granted(it) })
        return true
    }

    /** From MainActivity.onActivityResult; true when it was ours. */
    fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?): Boolean {
        when (requestCode) {
            REQ_TREE -> {
                val r = pendingTree ?: return true
                pendingTree = null
                val uri = data?.data
                if (resultCode != Activity.RESULT_OK || uri == null) {
                    r.success(null)
                    return true
                }
                try {
                    context.contentResolver.takePersistableUriPermission(uri, Intent.FLAG_GRANT_READ_URI_PERMISSION)
                } catch (_: Exception) {
                }
                pool.execute {
                    val label = PhoneBackupData.treeLabel(context, uri)
                    main.post { r.success(mapOf("uri" to uri.toString(), "label" to label)) }
                }
                return true
            }
            REQ_SMS_ROLE -> {
                val r = pendingRole ?: return true
                pendingRole = null
                r.success(isDefaultSms())
                return true
            }
        }
        return false
    }
}
