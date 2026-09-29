package com.fenyx.nivaroos_mobile

import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.Uri
import android.os.Handler
import android.os.Looper
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel

/**
 * The phone's network for lib/services/server_routes.dart:
 *
 * - "com.fenyx.nivaroos/network_events" sends an event whenever the
 *   default network changes (Wi-Fi to mobile data, Tailscale's VPN on or
 *   off), so the app checks which of the server's addresses answers.
 * - "com.fenyx.nivaroos/network" opens the Tailscale app, or its store page
 *   when it isn't installed ("Can't reach the server - turn on Tailscale").
 */
object NetworkChannels {
    private const val TAILSCALE = "com.tailscale.ipn"

    fun register(context: Context, engine: FlutterEngine) {
        val app = context.applicationContext
        EventChannel(engine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/network_events").setStreamHandler(object : EventChannel.StreamHandler {
            private var callback: ConnectivityManager.NetworkCallback? = null
            private val main = Handler(Looper.getMainLooper())

            override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
                val cm = app.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return
                fun send(what: String) {
                    main.post { events?.success(what) }
                }
                val cb = object : ConnectivityManager.NetworkCallback() {
                    override fun onAvailable(network: Network) {
                        send("available")
                    }

                    override fun onLost(network: Network) {
                        send("lost")
                    }

                    override fun onLinkPropertiesChanged(network: Network, linkProperties: LinkProperties) {
                        send("link")
                    }
                    override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                        // Signal strength changes arrive here too: only the
                        // kind of network matters.
                        val vpn = caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
                        val wifi = caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)
                        val kind = "caps:$vpn:$wifi"
                        if (kind != lastKind) {
                            lastKind = kind
                            send(kind)
                        }
                    }
                    private var lastKind: String? = null
                }
                try {
                    cm.registerDefaultNetworkCallback(cb)
                    callback = cb
                } catch (_: Exception) {
                }
            }

            override fun onCancel(arguments: Any?) {
                val cm = app.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
                callback?.let { try { cm?.unregisterNetworkCallback(it) } catch (_: Exception) {} }
                callback = null
            }
        })

        MethodChannel(engine.dartExecutor.binaryMessenger, "com.fenyx.nivaroos/network").setMethodCallHandler { call, result ->
            when (call.method) {
                "openTailscale" -> result.success(openTailscale(context))
                else -> result.notImplemented()
            }
        }
    }

    private fun openTailscale(context: Context): Boolean {
        val launch = context.packageManager.getLaunchIntentForPackage(TAILSCALE)
        val tries = listOfNotNull(
            launch,
            Intent(Intent.ACTION_VIEW, Uri.parse("market://details?id=$TAILSCALE")),
            Intent(Intent.ACTION_VIEW, Uri.parse("https://play.google.com/store/apps/details?id=$TAILSCALE")),
        )
        for (intent in tries) {
            try {
                intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                context.startActivity(intent)
                return true
            } catch (_: Exception) {
            }
        }
        return false
    }
}
