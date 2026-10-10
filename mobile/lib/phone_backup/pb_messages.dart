// Messages and call log in the SMS Backup & Restore XML format (device API
// §7), with the item keys the server dedups by:
//
//   sms:  sha256("sms|"  + address + "|" + date + "|" + type    + "|" + body)
//   mms:  sha256("mms|"  + address + "|" + date + "|" + msg_box + "|" + m_id)
//   call: sha256("call|" + number  + "|" + date + "|" + duration)
//
// The values are exactly the attribute values written to the XML (after
// unescaping), so a key is always computed from the item's own attribute
// map, after the text has been cleaned of characters XML 1.0 can't carry.
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:intl/intl.dart';

String _sha(String s) => sha256.convert(utf8.encode(s)).toString();

String smsKey(String address, String date, String type, String body) => _sha('sms|$address|$date|$type|$body');
String mmsKey(String address, String date, String msgBox, String mId) => _sha('mms|$address|$date|$msgBox|$mId');
String callKey(String number, String date, String duration) => _sha('call|$number|$date|$duration');

/// Drops the characters XML 1.0 can't hold (NUL and the other C0 controls
/// but tab, newline and carriage return; lone surrogates; U+FFFE/FFFF).
String xmlSafe(String s) {
  final b = StringBuffer();
  final runes = s.runes;
  for (final r in runes) {
    final ok = r == 0x9 || r == 0xA || r == 0xD || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || (r >= 0x10000 && r <= 0x10FFFF);
    if (ok) b.writeCharCode(r);
  }
  return b.toString();
}

/// An attribute value, escaped (whitespace other than spaces as character
/// references so no parser normalises it away).
String xmlAttr(String s) {
  final b = StringBuffer();
  for (final r in s.runes) {
    switch (r) {
      case 0x26:
        b.write('&amp;');
      case 0x3C:
        b.write('&lt;');
      case 0x3E:
        b.write('&gt;');
      case 0x22:
        b.write('&quot;');
      case 0x27:
        b.write('&apos;');
      case 0x9:
        b.write('&#9;');
      case 0xA:
        b.write('&#10;');
      case 0xD:
        b.write('&#13;');
      default:
        b.writeCharCode(r);
    }
  }
  return b.toString();
}

String _s(Object? v) => v == null ? 'null' : xmlSafe(v.toString());
String _n(Object? v, [String fallback = '0']) {
  if (v == null) return fallback;
  if (v is num) return v.toInt().toString();
  final t = v.toString();
  return int.tryParse(t)?.toString() ?? fallback;
}

final _readable = DateFormat('MMM d, yyyy h:mm:ss a', 'en_US');
String _readableDate(int ms) => ms <= 0 ? '' : _readable.format(DateTime.fromMillisecondsSinceEpoch(ms));

/// One `<sms>`, `<mms>` or `<call>` element.
class MessageItem {
  MessageItem(this.element, this.attrs, {this.parts = const [], this.addrs = const [], this.providerId = 0});

  final String element;

  /// In the order they are written.
  final Map<String, String> attrs;
  final List<Map<String, String>> parts;
  final List<Map<String, String>> addrs;

  /// The row's _id in the phone's provider (MMS parts are read by it).
  final int providerId;

  String get key => switch (element) {
        'sms' => smsKey(attrs['address'] ?? '', attrs['date'] ?? '', attrs['type'] ?? '', attrs['body'] ?? ''),
        'mms' => mmsKey(attrs['address'] ?? '', attrs['date'] ?? '', attrs['msg_box'] ?? '', attrs['m_id'] ?? ''),
        _ => callKey(attrs['number'] ?? '', attrs['date'] ?? '', attrs['duration'] ?? ''),
      };

  int get dateMs => int.tryParse(attrs['date'] ?? '') ?? 0;

  MessageItem withParts(List<Map<String, String>> p) => MessageItem(element, attrs, parts: p, addrs: addrs, providerId: providerId);

  void writeTo(StringSink out) {
    out.write('  <$element');
    attrs.forEach((k, v) => out.write(' $k="${xmlAttr(v)}"'));
    if (element != 'mms') {
      out.write(' />\n');
      return;
    }
    out.write('>\n    <parts>\n');
    for (final p in parts) {
      out.write('      <part');
      p.forEach((k, v) => out.write(' $k="${xmlAttr(v)}"'));
      out.write(' />\n');
    }
    out.write('    </parts>\n    <addrs>\n');
    for (final a in addrs) {
      out.write('      <addr');
      a.forEach((k, v) => out.write(' $k="${xmlAttr(v)}"'));
      out.write(' />\n');
    }
    out.write('    </addrs>\n  </mms>\n');
  }
}

/// A row of content://sms (as the platform side reads it).
MessageItem smsFromRow(Map<Object?, Object?> r) {
  final date = _n(r['date']);
  final ms = int.tryParse(date) ?? 0;
  return MessageItem('sms', {
    'protocol': _n(r['protocol']),
    'address': _s(r['address']),
    'date': date,
    'type': _n(r['type'], '1'),
    'subject': _s(r['subject']),
    'body': r['body'] == null ? '' : xmlSafe(r['body'].toString()),
    'toa': 'null',
    'sc_toa': 'null',
    'service_center': _s(r['service_center']),
    'read': _n(r['read'], '1'),
    'status': _n(r['status'], '-1'),
    'locked': _n(r['locked']),
    'date_sent': _n(r['date_sent']),
    'sub_id': _n(r['sub_id'], '-1'),
    'readable_date': _readableDate(ms),
    'contact_name': '(Unknown)',
  }, providerId: int.tryParse(_n(r['_id'])) ?? 0);
}

/// Address types of an MMS (PduHeaders): from, to, cc, bcc.
const mmsAddrFrom = 137, mmsAddrTo = 151;

/// A row of content://mms with its `addrs` (and, once read, `parts`).
/// MMS dates are seconds in the provider and milliseconds in the file.
MessageItem mmsFromRow(Map<Object?, Object?> r) {
  final secs = int.tryParse(_n(r['date'])) ?? 0;
  final ms = secs < 100000000000 ? secs * 1000 : secs;
  final box = _n(r['msg_box'], '1');
  final addrs = <Map<String, String>>[
    for (final a in (r['addrs'] is List ? r['addrs'] as List : const []))
      if (a is Map) {'address': _s(a['address']), 'type': _n(a['type'], '$mmsAddrTo'), 'charset': _n(a['charset'], '106')},
  ];
  // The other party: the sender of a received message, the recipients of
  // a sent one ("~" between several), as SMS Backup & Restore writes it.
  final inbox = box == '1';
  final others = addrs
      .where((a) => inbox ? a['type'] == '$mmsAddrFrom' : a['type'] != '$mmsAddrFrom')
      .map((a) => a['address']!)
      .where((a) => a != 'null' && a.isNotEmpty && a != 'insert-address-token')
      .toList();
  return MessageItem(
    'mms',
    {
      'date': '$ms',
      'ct_t': _s(r['ct_t']),
      'msg_box': box,
      'address': others.isEmpty ? 'null' : others.join('~'),
      'sub': _s(r['sub']),
      'm_id': _s(r['m_id']),
      'read': _n(r['read'], '1'),
      'm_type': _n(r['m_type'], '132'),
      'text_only': _n(r['text_only']),
      'sub_id': _n(r['sub_id'], '-1'),
      'date_sent': _n(r['date_sent']),
      'locked': _n(r['locked']),
      'readable_date': _readableDate(ms),
      'contact_name': '(Unknown)',
    },
    addrs: addrs,
    parts: [for (final p in (r['parts'] is List ? r['parts'] as List : const [])) if (p is Map) mmsPart(p)],
    providerId: int.tryParse(_n(r['_id'])) ?? 0,
  );
}

/// One part of an MMS; `data` (bytes) becomes base64.
Map<String, String> mmsPart(Map<Object?, Object?> p) {
  final data = p['data'];
  return {
    'seq': _n(p['seq']),
    'ct': _s(p['ct']),
    'name': _s(p['name']),
    'chset': _s(p['chset']),
    'cd': _s(p['cd']),
    'fn': _s(p['fn']),
    'cid': _s(p['cid']),
    'cl': _s(p['cl']),
    'ctt_s': 'null',
    'ctt_t': 'null',
    'text': _s(p['text']),
    if (data is List<int> && data.isNotEmpty) 'data': base64.encode(data),
  };
}

/// A row of CallLog.Calls.
MessageItem callFromRow(Map<Object?, Object?> r) {
  final date = _n(r['date']);
  return MessageItem('call', {
    'number': _s(r['number']),
    'duration': _n(r['duration']),
    'date': date,
    'type': _n(r['type'], '1'),
    'presentation': _n(r['presentation'], '1'),
    'subscription_id': _s(r['subscription_id']),
    'post_dial_digits': r['post_dial_digits'] == null ? '' : xmlSafe(r['post_dial_digits'].toString()),
    'readable_date': _readableDate(int.tryParse(date) ?? 0),
    'contact_name': r['name'] == null || r['name'].toString().isEmpty ? '(Unknown)' : xmlSafe(r['name'].toString()),
  }, providerId: int.tryParse(_n(r['_id'])) ?? 0);
}

/// Writes an SMS Backup & Restore file with [items] (sms and mms) to [file].
Future<void> writeSmsXml(File file, List<MessageItem> items, {DateTime? now}) =>
    _write(file, 'smses', items, now: now);

/// Writes a call log file.
Future<void> writeCallsXml(File file, List<MessageItem> items, {DateTime? now}) =>
    _write(file, 'calls', items, now: now);

Future<void> _write(File file, String root, List<MessageItem> items, {DateTime? now}) async {
  final sink = file.openWrite(encoding: utf8);
  try {
    sink.write("<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>\n");
    final at = (now ?? DateTime.now()).millisecondsSinceEpoch;
    sink.write('<$root count="${items.length}" backup_date="$at" type="incremental">\n');
    final buf = StringBuffer();
    for (final i in items) {
      i.writeTo(buf);
      if (buf.length > 256 << 10) {
        sink.write(buf.toString());
        buf.clear();
      }
    }
    sink.write(buf.toString());
    sink.write('</$root>\n');
  } finally {
    await sink.close();
  }
}

// --- MMS restore ----------------------------------------------------------

final _attrRe = RegExp(r'''([A-Za-z_][\w:.-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')''');
final _entityRe = RegExp(r'&(#x[0-9A-Fa-f]+|#[0-9]+|amp|lt|gt|quot|apos);');

String _unescape(String s) => !s.contains('&')
    ? s
    : s.replaceAllMapped(_entityRe, (m) {
        final e = m[1]!;
        if (e.startsWith('#')) {
          final code = e.startsWith('#x') ? int.tryParse(e.substring(2), radix: 16) : int.tryParse(e.substring(1));
          return code == null ? m[0]! : String.fromCharCode(code);
        }
        return const {'amp': '&', 'lt': '<', 'gt': '>', 'quot': '"', 'apos': "'"}[e]!;
      });

Map<String, String> _attrs(String tag) => {for (final m in _attrRe.allMatches(tag)) m[1]!: _unescape(m[2] ?? m[3] ?? '')};

/// One `<mms ...>...</mms>` element of an SMS Backup & Restore file.
MessageItem parseMmsElement(String xml) {
  final end = xml.indexOf('>');
  return MessageItem(
    'mms',
    _attrs(xml.substring(0, end < 0 ? xml.length : end)),
    parts: [for (final m in RegExp(r'<part\b([^>]*)>').allMatches(xml)) _attrs(m[1]!)],
    addrs: [for (final m in RegExp(r'<addr\b([^>]*)>').allMatches(xml)) _attrs(m[1]!)],
  );
}

/// The `<mms>` elements of an SMS Backup & Restore file, read in chunks
/// (attachments make these files large); `<sms>` elements are skipped.
Stream<MessageItem> readMmsXml(File file) async* {
  var buf = '';
  await for (final chunk in file.openRead().transform(utf8.decoder)) {
    buf += chunk;
    while (true) {
      final s = buf.indexOf(RegExp(r'<mms[\s>/]'));
      if (s < 0) {
        // Keep a tail that may be the start of "<mms ".
        buf = buf.length > 4 ? buf.substring(buf.length - 4) : buf;
        break;
      }
      final tagEnd = buf.indexOf('>', s);
      if (tagEnd < 0) {
        buf = buf.substring(s);
        break;
      }
      final selfClosing = buf[tagEnd - 1] == '/';
      final e = selfClosing ? tagEnd + 1 : buf.indexOf('</mms>', tagEnd);
      if (e < 0) {
        buf = buf.substring(s);
        break;
      }
      final stop = selfClosing ? e : e + 6;
      yield parseMmsElement(buf.substring(s, stop));
      buf = buf.substring(stop);
    }
  }
}

/// The key an MMS is recognised by on the phone, so a restore skips the
/// ones already there (same rule as the backup's [mmsKey]). The provider
/// keeps seconds, a file may carry milliseconds: compared in seconds.
String mmsDedupKey(MessageItem m) {
  final ms = int.tryParse(m.attrs['date'] ?? '') ?? 0;
  return mmsKey(m.attrs['address'] ?? '', '${ms ~/ 1000 * 1000}', m.attrs['msg_box'] ?? '', m.attrs['m_id'] ?? '');
}

/// What insertMms writes: the content://mms row, its addresses and parts.
class MmsInsert {
  const MmsInsert({required this.pdu, required this.recipients, required this.addrs, required this.parts});

  /// Columns of content://mms (date in seconds).
  final Map<String, Object> pdu;

  /// The other party, for the conversation (Threads.getOrCreateThreadId).
  final List<String> recipients;
  final List<Map<String, Object>> addrs;

  /// Columns of `content://mms/<id>/part`; `data` (bytes) is the file.
  final List<Map<String, Object>> parts;

  Map<String, Object> toArgs() => {'pdu': pdu, 'recipients': recipients, 'addrs': addrs, 'parts': parts};
}

const _pduText = ['m_id', 'sub', 'ct_t', 'ct_l', 'tr_id', 'm_cls'];
const _pduInt = ['date_sent', 'msg_box', 'read', 'm_type', 'text_only', 'sub_id', 'locked', 'sub_cs', 'v', 'pri', 'rr', 'd_rpt', 'exp', 'm_size', 'resp_st', 'st', 'retr_st', 'read_status', 'ct_cls'];
const _partText = ['ct', 'name', 'cd', 'fn', 'cid', 'cl', 'ctt_t', 'text'];
const _partInt = ['seq', 'chset', 'ctt_s'];

bool _has(String? v) => v != null && v != 'null';

/// An `<mms>` of a backup file as the rows to insert. "null" values are
/// left out; only known columns are written (the provider rejects others).
MmsInsert mmsInsertOf(MessageItem m) {
  Map<String, Object> pick(Map<String, String> a, List<String> text, List<String> ints) => {
        for (final k in text)
          if (_has(a[k])) k: a[k]!,
        for (final k in ints)
          if (_has(a[k]) && int.tryParse(a[k]!) != null) k: int.parse(a[k]!),
      };
  final a = m.attrs;
  final ms = int.tryParse(a['date'] ?? '') ?? 0;
  final pdu = {...pick(a, _pduText, _pduInt), 'date': ms ~/ 1000, 'seen': 1};
  pdu.putIfAbsent('msg_box', () => 1);
  pdu.putIfAbsent('read', () => 1);
  final addrs = [
    for (final x in m.addrs)
      if (_has(x['address'])) {'address': x['address']!, 'type': int.tryParse(x['type'] ?? '') ?? mmsAddrTo, 'charset': int.tryParse(x['charset'] ?? '') ?? 106},
  ];
  bool real(String s) => s.isNotEmpty && s != 'null' && s != 'insert-address-token';
  var recipients = (a['address'] ?? '').split('~').where(real).toList();
  if (recipients.isEmpty) recipients = [for (final x in addrs) x['address'] as String].where(real).toSet().toList();
  final parts = [
    for (final p in m.parts)
      {
        ...pick(p, _partText, _partInt),
        if (_has(p['data']) && p['data']!.isNotEmpty) 'data': base64.decode(p['data']!),
      },
  ];
  return MmsInsert(pdu: pdu, recipients: recipients, addrs: addrs, parts: parts);
}
