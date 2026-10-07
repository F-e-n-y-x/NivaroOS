import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'download_station_api.dart' show TorrentFile;
import 'share_upload.dart';

/// Android's share sheet, both ways (MainActivity.kt):
/// - in: text another app shared to "Download on server" (a link from
///   Chrome, or a magnet link) waits in [pending] until the shell offers
///   to download it, also when the share is what started the app; a
///   magnet: link or .torrent file opened with NivaroOS (ACTION_VIEW)
///   arrives the same way, the file in [pendingTorrent];
/// - in: files shared to "Upload to NivaroOS" wait in [pendingFiles];
///   a tap on an upload's notification in [pendingOpen];
/// - out: [shareText] opens the share sheet with a link.
abstract final class ShareIntent {
  static const _channel = MethodChannel('com.fenyx.nivaroos/share_intent');

  /// Shared text not handled yet; the shell clears it once it has.
  static final ValueNotifier<String?> pending = ValueNotifier(null);

  /// A .torrent file opened with the app, not handled yet.
  static final ValueNotifier<TorrentFile?> pendingTorrent = ValueNotifier(null);

  /// Files shared to "Upload to NivaroOS", not handled yet.
  static final ValueNotifier<SharedFiles?> pendingFiles = ValueNotifier(null);

  /// An upload's notification was tapped: its batch, and the folder to
  /// open when it went well.
  static final ValueNotifier<({String batch, String? folder})?> pendingOpen = ValueNotifier(null);

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
      apply(await _channel.invokeMethod<Object>('take'));
    } catch (_) {
      // Not on Android (tests, iOS): nothing is ever shared in.
    }
  }

  /// Files out what MainActivity handed over: shared text, a .torrent
  /// file, shared files or a tapped upload notification.
  @visibleForTesting
  static void apply(Object? v) {
    if (v is String && v.trim().isNotEmpty) pending.value = v;
    if (v is! Map) return;
    if (v['data'] is Uint8List) pendingTorrent.value = TorrentFile(v['name']?.toString() ?? 'file.torrent', v['data'] as Uint8List);
    final files = SharedFiles.fromPayload(v);
    if (files != null && (files.files.isNotEmpty || files.rejected > 0)) pendingFiles.value = files;
    if (v['open'] is String) pendingOpen.value = (batch: v['open'] as String, folder: v['folder'] is String ? v['folder'] as String : null);
  }

  /// A picture or video's thumbnail (JPEG), null when there is none.
  static Future<Uint8List?> thumbnail(String uri, {int px = 128}) async {
    try {
      return await _channel.invokeMethod<Uint8List>('thumbnail', {'uri': uri, 'px': px});
    } catch (_) {
      return null;
    }
  }

  /// Hands a saved batch to Android's job scheduler; false when it refused.
  static Future<bool> startUpload(ShareBatch b) async {
    try {
      return await _channel.invokeMethod<bool>('startUpload', {
            'batch': b.id,
            'uris': [for (final i in b.items) if (i.state == ShareItemState.pending) i.file.uri],
            'bytes': b.totalBytes,
          }) ??
          false;
    } catch (_) {
      return false;
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
