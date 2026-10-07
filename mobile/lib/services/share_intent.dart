import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'download_station_api.dart' show TorrentFile;

/// Android's share sheet, both ways (MainActivity.kt):
/// - in: text another app shared to "Download on server" (a link from
///   Chrome, or a magnet link) waits in [pending] until the shell offers
///   to download it, also when the share is what started the app; a
///   magnet: link or .torrent file opened with NivaroOS (ACTION_VIEW)
///   arrives the same way, the file in [pendingTorrent];
/// - out: [shareText] opens the share sheet with a link.
abstract final class ShareIntent {
  static const _channel = MethodChannel('com.fenyx.nivaroos/share_intent');

  /// Shared text not handled yet; the shell clears it once it has.
  static final ValueNotifier<String?> pending = ValueNotifier(null);

  /// A .torrent file opened with the app, not handled yet.
  static final ValueNotifier<TorrentFile?> pendingTorrent = ValueNotifier(null);

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
      final v = await _channel.invokeMethod<Object>('take');
      if (v is String && v.trim().isNotEmpty) pending.value = v;
      if (v is Map && v['data'] is Uint8List) pendingTorrent.value = TorrentFile(v['name']?.toString() ?? 'file.torrent', v['data'] as Uint8List);
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
