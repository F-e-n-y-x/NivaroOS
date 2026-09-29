package com.fenyx.nivaroos_mobile

import android.app.Activity
import android.app.Notification
import android.app.NotificationManager
import android.app.Service
import android.content.BroadcastReceiver
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.os.Build
import android.os.Bundle
import android.os.IBinder
import android.provider.Settings
import android.provider.Telephony
import android.widget.Toast

/*
 * Android lets only the default SMS app write messages, so restoring SMS
 * means making NivaroOS the default SMS app for the few minutes of the
 * restore (the Restore screen explains this and switches back after). To
 * be offered as the default, an app must have these four components. They
 * do the least that keeps nothing lost while NivaroOS holds the role: an
 * incoming SMS is saved to the inbox and a notification asks to switch
 * back; NivaroOS never sends messages.
 */

private const val SMS_NOTIFICATION_ID = 42853

private fun switchBackNotice(context: Context, text: String) {
    PhoneBackupJobService.ensureChannel(context)
    val b = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) Notification.Builder(context, PhoneBackupJobService.CHANNEL_ID) else {
        @Suppress("DEPRECATION")
        Notification.Builder(context)
    }
    val settings = android.app.PendingIntent.getActivity(
        context, 9,
        Intent(Settings.ACTION_MANAGE_DEFAULT_APPS_SETTINGS).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        android.app.PendingIntent.FLAG_IMMUTABLE or android.app.PendingIntent.FLAG_UPDATE_CURRENT,
    )
    b.setSmallIcon(Icon.createWithResource(context, R.drawable.ic_stat_nivaroos))
        .setContentTitle("Switch back to your messaging app")
        .setContentText(text)
        .setContentIntent(settings)
        .setAutoCancel(true)
    try {
        context.getSystemService(NotificationManager::class.java)?.notify(SMS_NOTIFICATION_ID, b.build())
    } catch (_: Exception) {
    }
}

/** SMS_DELIVER: only the default SMS app gets it. Saved to the inbox. */
class SmsDeliverReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Telephony.Sms.Intents.SMS_DELIVER_ACTION) return
        try {
            val parts = Telephony.Sms.Intents.getMessagesFromIntent(intent) ?: return
            if (parts.isEmpty()) return
            val body = parts.joinToString("") { it.messageBody ?: "" }
            val first = parts[0]
            val v = ContentValues().apply {
                put(Telephony.Sms.ADDRESS, first.originatingAddress)
                put(Telephony.Sms.BODY, body)
                put(Telephony.Sms.DATE, System.currentTimeMillis())
                put(Telephony.Sms.DATE_SENT, first.timestampMillis)
                put(Telephony.Sms.READ, 0)
                put(Telephony.Sms.SEEN, 0)
                put(Telephony.Sms.TYPE, Telephony.Sms.MESSAGE_TYPE_INBOX)
                first.serviceCenterAddress?.let { put(Telephony.Sms.SERVICE_CENTER, it) }
            }
            context.contentResolver.insert(Telephony.Sms.Inbox.CONTENT_URI, v)
        } catch (_: Exception) {
        }
        switchBackNotice(context, "A message arrived while NivaroOS restores your messages. It is saved; make your usual app the default again.")
    }
}

/** WAP_PUSH_DELIVER (MMS): NivaroOS can't download MMS; it asks to switch back at once. */
class MmsDeliverReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        switchBackNotice(context, "A picture message is waiting. Make your usual messaging app the default again to receive it.")
    }
}

/** RESPOND_VIA_MESSAGE ("reply with a message" to a call): not handled. */
class SmsRespondService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null
    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        stopSelf(startId)
        return START_NOT_STICKY
    }
}

/** SENDTO sms:/mms: - NivaroOS doesn't write messages; send the user back. */
class SmsComposeActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Toast.makeText(this, "NivaroOS only restores messages. Make your usual messaging app the default again to write one.", Toast.LENGTH_LONG).show()
        try {
            startActivity(Intent(Settings.ACTION_MANAGE_DEFAULT_APPS_SETTINGS))
        } catch (_: Exception) {
        }
        finish()
    }
}
