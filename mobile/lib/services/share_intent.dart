import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Android's share sheet, both ways (MainActivity.kt):
/// - in: text another app shared to "Download on server" (a link from
///   Chrome) waits in [pending] until the shell offers to download it,
///   also when the share is what started the app;
/// - out: [shareText] opens the share sheet with a link.
abstract final class ShareIntent {
  static const _channel = MethodChannel('com.fenyx.nivaroos/share_intent');

  /// Shared text not handled yet; the shell clears it once it has.
  static final ValueNotifier<String?> pending = ValueNotifier(null);

  static bool _listening = false;

  /// Picks up a share that started the app and listens for later ones.
  static Future<void> listen() async {
    if (!_listening) {
      _listening = true;
      _channel.setMethodCallHandler((call) async {
        if (call.method == 'shared') await _take();
      });
    }
    await _take();
  }

  static Future<void> _take() async {
    try {
      final text = await _channel.invokeMethod<String>('take');
      if (text != null && text.trim().isNotEmpty) pending.value = text;
    } catch (_) {
      // Not on Android (tests, iOS): nothing is ever shared in.
    }
  }

  /// Opens the share sheet for [text]; false when it couldn't.
  static Future<bool> shareText(String text, {String? title}) async {
    try {
      return await _channel.invokeMethod<bool>('shareText', {'text': text, 'title': ?title}) ?? false;
    } catch (_) {
      return false;
    }
  }
}
