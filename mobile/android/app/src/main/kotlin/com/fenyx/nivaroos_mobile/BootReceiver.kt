package com.fenyx.nivaroos_mobile

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val action = intent.action
        if (action == Intent.ACTION_BOOT_COMPLETED ||
            action == "android.intent.action.QUICKBOOT_POWERON" ||
            action == "com.htc.intent.action.QUICKBOOT_POWERON") {

            val prefs = context.getSharedPreferences("nivaroos_bg_prefs", Context.MODE_PRIVATE)
            val autoStart = prefs.getBoolean("auto_start_boot", true)
            if (autoStart) {
                BackgroundCompanionService.start(
                    context,
                    "NivaroOS Companion Active",
                    "Started on device boot · Unattended background sync"
                )
            }
        }
    }
}
