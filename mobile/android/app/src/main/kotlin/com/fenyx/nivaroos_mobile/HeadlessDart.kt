package com.fenyx.nivaroos_mobile

import android.content.Context
import io.flutter.FlutterInjector
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.dart.DartExecutor

/**
 * Starts a Flutter engine with no UI that runs one Dart function from
 * lib/services/background_sync_isolate.dart. The sharing service and the
 * heartbeat job use it: the Dart code (sign-in, token refresh, the file
 * server) is the same code the app runs, in its own isolate.
 *
 * Plugins register themselves on the new engine (FlutterEngine does that by
 * default), so secure storage, device info and battery work there too.
 */
object HeadlessDart {
    private const val LIBRARY = "package:nivaroos_mobile/services/background_sync_isolate.dart"

    /** Must be called on the main thread. [configure] runs before Dart starts. */
    fun start(context: Context, function: String, configure: (FlutterEngine) -> Unit): FlutterEngine {
        val app = context.applicationContext
        val loader = FlutterInjector.instance().flutterLoader()
        if (!loader.initialized()) loader.startInitialization(app)
        loader.ensureInitializationComplete(app, null)
        val engine = FlutterEngine(app)
        configure(engine)
        engine.dartExecutor.executeDartEntrypoint(
            DartExecutor.DartEntrypoint(loader.findAppBundlePath(), LIBRARY, function)
        )
        return engine
    }
}
