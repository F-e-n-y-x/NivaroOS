/// What the web's "Pair a phone" QR code holds (ui/src/utils/pairPhone.js):
///
///   nivaroos://pair?code=ABCDEFGH&url=http%3A%2F%2F192.168.1.20&url=...
///
/// The one-time code and the server's addresses, in the order to try them.
/// Never a token or password.
class PairLink {
  const PairLink({required this.code, required this.urls});

  final String code;
  final List<String> urls;

  static const codeLength = 8;
  static const _alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';

  /// The code as typed: spaces and the dash dropped, upper case.
  static String normalizeCode(String s) => s.toUpperCase().replaceAll(RegExp(r'[\s-]'), '');

  /// True for a whole code of the server's alphabet.
  static bool isCode(String s) {
    final c = normalizeCode(s);
    return c.length == codeLength && c.split('').every(_alphabet.contains);
  }

  /// ABCDEFGH -> ABCD-EFGH.
  static String formatCode(String s) {
    final c = normalizeCode(s);
    return c.length == codeLength ? '${c.substring(0, 4)}-${c.substring(4)}' : c;
  }

  /// A scanned QR code, or null when it isn't a NivaroOS pairing code.
  /// Only http(s) addresses are kept, at most 8.
  static PairLink? parse(String raw) {
    final uri = Uri.tryParse(raw.trim());
    if (uri == null || uri.scheme != 'nivaroos' || uri.host != 'pair') return null;
    final code = normalizeCode(uri.queryParameters['code'] ?? '');
    if (!isCode(code)) return null;
    final urls = <String>[];
    for (final u in uri.queryParametersAll['url'] ?? const <String>[]) {
      final p = Uri.tryParse(u);
      if (p != null && (p.scheme == 'http' || p.scheme == 'https') && p.host.isNotEmpty && !urls.contains(u)) urls.add(u);
      if (urls.length == 8) break;
    }
    return PairLink(code: code, urls: urls);
  }
}
