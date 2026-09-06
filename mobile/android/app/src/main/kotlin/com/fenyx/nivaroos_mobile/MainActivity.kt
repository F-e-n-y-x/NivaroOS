package com.fenyx.nivaroos_mobile

import android.content.Context
import android.net.wifi.WifiManager
import android.os.Bundle
import io.flutter.embedding.android.FlutterActivity

class MainActivity : FlutterActivity() {
    // multicast_dns (the pure-Dart mDNS package used for server auto-
    // discovery) opens a plain UDP socket - it has no native Android code
    // of its own, so nothing else acquires this. Without it, some devices
    // silently drop incoming multicast packets (Android disables multicast
    // reception by default for battery reasons), which would make
    // discovery "work" in testing on devices that happen not to enforce
    // this and mysteriously find nothing on others.
    private var multicastLock: WifiManager.MulticastLock? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val wifi = applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
        multicastLock = wifi?.createMulticastLock("nivaroos-mdns-discovery")?.apply {
            setReferenceCounted(true)
            acquire()
        }
    }

    override fun onDestroy() {
        multicastLock?.let { if (it.isHeld) it.release() }
        super.onDestroy()
    }
}
