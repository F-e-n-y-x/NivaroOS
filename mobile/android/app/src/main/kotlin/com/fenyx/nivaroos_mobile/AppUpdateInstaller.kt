package com.fenyx.nivaroos_mobile

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.content.pm.Signature
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.util.Log
import java.io.File
import java.security.MessageDigest

/**
 * Self-update from GitHub releases (plan WPR-2). Dart downloads the APK and
 * checks its SHA-256; this checks that the APK is this app, signed with the
 * same certificate as the installed app, and hands it to Android's
 * PackageInstaller. Android would refuse an update signed with another key
 * anyway, but only after the user confirmed - checking first gives a clear
 * message instead.
 */
object AppUpdateInstaller {
    private const val TAG = "AppUpdate"

    /** SHA-256 fingerprints (lower-case hex) of the installed app's signing certificates. */
    fun installedSigners(context: Context): List<String> {
        val pm = context.packageManager
        val info = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            pm.getPackageInfo(context.packageName, PackageManager.GET_SIGNING_CERTIFICATES)
        } else {
            @Suppress("DEPRECATION")
            pm.getPackageInfo(context.packageName, PackageManager.GET_SIGNATURES)
        }
        return signers(info)
    }

    /** Package name, version and signers of the APK at [path], or null if it isn't a readable APK. */
    fun apkInfo(context: Context, path: String): Map<String, Any?>? {
        val pm = context.packageManager
        val info: PackageInfo? = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            pm.getPackageArchiveInfo(path, PackageManager.GET_SIGNING_CERTIFICATES)
        } else {
            @Suppress("DEPRECATION")
            pm.getPackageArchiveInfo(path, PackageManager.GET_SIGNATURES)
        }
        if (info == null) return null
        val versionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) info.longVersionCode else {
            @Suppress("DEPRECATION")
            info.versionCode.toLong()
        }
        return mapOf(
            "packageName" to info.packageName,
            "versionName" to info.versionName,
            "versionCode" to versionCode,
            "signers" to signers(info),
        )
    }

    private fun signers(info: PackageInfo): List<String> {
        val sigs: Array<Signature>? = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            val si = info.signingInfo
            when {
                si == null -> null
                si.hasMultipleSigners() -> si.apkContentsSigners
                else -> si.signingCertificateHistory
            }
        } else {
            @Suppress("DEPRECATION")
            info.signatures
        }
        return sigs?.map { sha256(it.toByteArray()) } ?: emptyList()
    }

    /** SHA-256 (lower-case hex) of the file at [path], read in chunks. */
    fun fileSha256(path: String): String {
        val digest = MessageDigest.getInstance("SHA-256")
        File(path).inputStream().use { input ->
            val buffer = ByteArray(64 * 1024)
            while (true) {
                val n = input.read(buffer)
                if (n < 0) break
                digest.update(buffer, 0, n)
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }

    private fun sha256(bytes: ByteArray): String =
        MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }

    fun canRequestInstalls(context: Context): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.O || context.packageManager.canRequestPackageInstalls()

    fun openInstallSettings(context: Context) {
        val intent = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))
        } else {
            Intent(Settings.ACTION_SECURITY_SETTINGS)
        }
        context.startActivity(intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    }

    /**
     * Streams the APKs (base.apk first, then any splits of the same app) into
     * one PackageInstaller session and commits it; Android then asks the user.
     * The self-update installs this app; a phone-backup restore installs [appPackage].
     */
    fun install(context: Context, paths: List<String>, appPackage: String = context.packageName) {
        val installer = context.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
        params.setAppPackageName(appPackage)
        val sessionId = installer.createSession(params)
        try {
            installer.openSession(sessionId).use { session ->
                paths.forEachIndexed { i, path ->
                    val file = File(path)
                    file.inputStream().use { input ->
                        session.openWrite(if (i == 0) "base.apk" else "split_$i.apk", 0, file.length()).use { out ->
                            input.copyTo(out)
                            session.fsync(out)
                        }
                    }
                }
                var flags = PendingIntent.FLAG_UPDATE_CURRENT
                // The installer fills in the status extras, so the intent must be mutable.
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) flags = flags or PendingIntent.FLAG_MUTABLE
                val status = PendingIntent.getBroadcast(
                    context, sessionId,
                    Intent(context, InstallStatusReceiver::class.java).setPackage(context.packageName).putExtra(EXTRA_APP, appPackage),
                    flags
                )
                session.commit(status.intentSender)
            }
        } catch (e: Exception) {
            try {
                installer.abandonSession(sessionId)
            } catch (_: Exception) {
            }
            throw e
        }
    }

    private const val EXTRA_APP = "com.fenyx.nivaroos.APP"

    /** Status of an install: asks the user to confirm, or records why it failed. */
    class InstallStatusReceiver : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
                PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                    @Suppress("DEPRECATION")
                    val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT)
                    if (confirm != null) context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                }
                PackageInstaller.STATUS_SUCCESS -> Unit
                else -> {
                    val message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE) ?: "status $status"
                    Log.w(TAG, "Install failed: $message")
                    val app = intent.getStringExtra(EXTRA_APP)
                    if (app == null || app == context.packageName) {
                        context.getSharedPreferences("nivaroos_update", Context.MODE_PRIVATE)
                            .edit().putString("last_install_error", message).apply()
                    } else if (status != PackageInstaller.STATUS_FAILURE_ABORTED) {
                        // A restored app (phone backup): the restore screen has no result to wait on.
                        android.widget.Toast.makeText(context, "Couldn't install $app: $message", android.widget.Toast.LENGTH_LONG).show()
                    }
                }
            }
        }
    }
}
