package com.fenyx.nivaroos_mobile

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.net.wifi.WifiManager
import android.os.BatteryManager
import android.os.Build
import android.os.Bundle
import android.os.PowerManager
import android.provider.Settings
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private var multicastLock: WifiManager.MulticastLock? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val wifi = applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
        multicastLock = wifi?.createMulticastLock("nivaroos-mdns-discovery")?.apply {
            setReferenceCounted(true)
            acquire()
        }
    }

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        // Device Info channel
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/device_info").setMethodCallHandler { call, result ->
            if (call.method == "getBatteryLevel") {
                try {
                    val bm = getSystemService(Context.BATTERY_SERVICE) as? BatteryManager
                    val level = bm?.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY) ?: -1
                    result.success(level)
                } catch (e: Exception) {
                    result.success(-1)
                }
            } else {
                result.notImplemented()
            }
        }

        // Background Service & Unattended Execution channel
        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/background_service").setMethodCallHandler { call, result ->
            val prefs = getSharedPreferences("nivaroos_bg_prefs", Context.MODE_PRIVATE)
            when (call.method) {
                "startService" -> {
                    val title = call.argument<String>("title")
                    val message = call.argument<String>("message")
                    BackgroundCompanionService.start(this, title, message)
                    prefs.edit().putBoolean("bg_service_enabled", true).apply()
                    result.success(true)
                }
                "stopService" -> {
                    BackgroundCompanionService.stop(this)
                    prefs.edit().putBoolean("bg_service_enabled", false).apply()
                    result.success(true)
                }
                "isServiceRunning" -> {
                    result.success(BackgroundCompanionService.isRunning)
                }
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
                "isAutoStartOnBoot" -> {
                    val autoStart = prefs.getBoolean("auto_start_boot", true)
                    result.success(autoStart)
                }
                "setAutoStartOnBoot" -> {
                    val enabled = call.argument<Boolean>("enabled") ?: true
                    prefs.edit().putBoolean("auto_start_boot", enabled).apply()
                    result.success(true)
                }
                else -> result.notImplemented()
            }
        }
    }

    override fun onDestroy() {
        multicastLock?.let { if (it.isHeld) it.release() }
        super.onDestroy()
    }
}
