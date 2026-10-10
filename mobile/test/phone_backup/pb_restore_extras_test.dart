// MMS write-back (SMS Backup & Restore XML -> provider rows, duplicates
// skipped) and which backed-up APKs are worth reinstalling.
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/phone_backup/pb_messages.dart';
import 'package:nivaroos_mobile/phone_backup/pb_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_restore.dart';

/// A provider row of content://mms as readMms returns it (date in seconds).
Map<Object?, Object?> mmsRow(int id, {int secs = 1700000000, int box = 1, String mId = '<a@mms>', List<int>? jpeg}) => {
      '_id': id,
      'date': secs,
      'date_sent': secs - 5,
      'msg_box': box,
      'm_id': mId,
      'sub': null,
      'ct_t': 'application/vnd.wap.multipart.related',
      'read': 1,
      'm_type': box == 1 ? 132 : 128,
      'text_only': 0,
      'sub_id': 1,
      'locked': 0,
      'addrs': [
        {'address': '+15550100', 'type': 137, 'charset': 106},
        {'address': 'insert-address-token', 'type': 151, 'charset': 106},
      ],
      'parts': [
        {'seq': -1, 'ct': 'application/smil', 'text': '<smil><body/></smil>'},
        {'seq': 0, 'ct': 'text/plain', 'chset': 106, 'text': 'Look & see <3'},
        if (jpeg != null) {'seq': 0, 'ct': 'image/jpeg', 'name': 'pic.jpg', 'cl': 'pic.jpg', 'data': jpeg},
      ],
    };

void main() {
  late Directory dir;
  setUp(() => dir = Directory.systemTemp.createTempSync('pbx'));
  tearDown(() => dir.deleteSync(recursive: true));

  Future<File> backupFile(List<MessageItem> items) async {
    final f = File('${dir.path}/sms.xml');
    await writeSmsXml(f, items, now: DateTime.utc(2026, 10, 1));
    return f;
  }

  group('MMS from the backup file', () {
    test('the file read back gives the rows to insert', () async {
      final jpeg = List<int>.generate(300, (i) => i % 256);
      final f = await backupFile([
        smsFromRow({'_id': 1, 'address': '+15550100', 'date': 1700000000000, 'type': 1, 'body': 'plain <mms> text'}),
        mmsFromRow(mmsRow(7, jpeg: jpeg)),
      ]);
      final got = await readMmsXml(f).toList();
      expect(got, hasLength(1));
      final m = mmsInsertOf(got.single);
      expect(m.pdu['date'], 1700000000);
      expect(m.pdu['date_sent'], 1699999995);
      expect(m.pdu['msg_box'], 1);
      expect(m.pdu['m_id'], '<a@mms>');
      expect(m.pdu['seen'], 1);
      expect(m.pdu.containsKey('sub'), isFalse, reason: '"null" is left out');
      expect(m.pdu.keys, isNot(contains('readable_date')));
      expect(m.recipients, ['+15550100']);
      expect(m.addrs, [
        {'address': '+15550100', 'type': 137, 'charset': 106},
        {'address': 'insert-address-token', 'type': 151, 'charset': 106},
      ]);
      expect(m.parts, hasLength(3));
      expect(m.parts[1], {'ct': 'text/plain', 'text': 'Look & see <3', 'seq': 0, 'chset': 106});
      expect(m.parts[2]['data'], Uint8List.fromList(jpeg));
      expect(m.parts[2]['name'], 'pic.jpg');
      expect(m.parts[0].containsKey('data'), isFalse);
    });

    test('a file from SMS Backup & Restore: quotes, entities, milliseconds, several recipients', () async {
      final f = File('${dir.path}/other.xml')
        ..writeAsStringSync("<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>\n<smses count='1'>"
            "<mms date='1700000000123' msg_box='2' address='+15550100~+15550101' m_id='null' ct_t='application/vnd.wap.multipart.related' sub='Hi &amp; bye &#233;'>"
            "<parts><part seq='0' ct='text/plain' text='a&#10;b' /></parts>"
            "<addrs><addr address='+15550199' type='137' charset='106' /><addr address='+15550100' type='151' charset='106' /></addrs></mms></smses>");
      final m = mmsInsertOf((await readMmsXml(f).toList()).single);
      expect(m.pdu['date'], 1700000000);
      expect(m.pdu['sub'], 'Hi & bye é');
      expect(m.pdu.containsKey('m_id'), isFalse);
      expect(m.recipients, ['+15550100', '+15550101']);
      expect(m.parts.single['text'], 'a\nb');
    });

    test('an attachment bigger than a read chunk', () async {
      final big = List<int>.generate(400000, (i) => (i * 7) % 256);
      final f = await backupFile([mmsFromRow(mmsRow(1, jpeg: big)), mmsFromRow(mmsRow(2, mId: '<b@mms>'))]);
      final got = await readMmsXml(f).toList();
      expect(got, hasLength(2));
      expect(mmsInsertOf(got.first).parts.last['data'], Uint8List.fromList(big));
      expect(got.last.attrs['m_id'], '<b@mms>');
    });
  });

  group('MMS restore', () {
    test('skips what the phone has and repeats in the file, counts failures', () async {
      final f = await backupFile([
        mmsFromRow(mmsRow(1)), // on the phone already
        mmsFromRow(mmsRow(2, mId: '<new@mms>')),
        mmsFromRow(mmsRow(3, mId: '<new@mms>')), // the same again
        mmsFromRow(mmsRow(4, mId: '<bad@mms>')),
      ]);
      final inserted = <String>[];
      final done = <int>[];
      final r = await restoreMms(
        f,
        readExisting: (off, limit) async => off == 0 ? [mmsRow(99)] : [],
        insert: (m) async {
          if (m.pdu['m_id'] == '<bad@mms>') throw PlatformException(code: 'FAILED');
          inserted.add(m.pdu['m_id'] as String);
        },
        onProgress: done.add,
      );
      expect(r, {'inserted': 1, 'skipped': 2, 'failed': 1});
      expect(inserted, ['<new@mms>']);
      expect(done, [1, 2, 3, 4]);
    });

    test('a message with a millisecond date is the same one after its round trip', () {
      final fromFile = parseMmsElement('<mms date="1700000000999" msg_box="1" address="+15550100" m_id="&lt;a@mms&gt;"><parts/><addrs/></mms>');
      expect(mmsDedupKey(fromFile), mmsDedupKey(mmsFromRow(mmsRow(1))));
      expect(mmsDedupKey(fromFile), isNot(mmsDedupKey(mmsFromRow(mmsRow(1, box: 2)))));
    });

    test('stops when NivaroOS is no longer the default SMS app', () async {
      final f = await backupFile([mmsFromRow(mmsRow(1)), mmsFromRow(mmsRow(2, mId: '<b@mms>'))]);
      expect(
        restoreMms(f, readExisting: (_, _) async => [], insert: (_) async => throw PlatformException(code: 'NOT_DEFAULT_SMS')),
        throwsA(isA<PlatformException>()),
      );
    });
  });

  group('apps to reinstall', () {
    RestoreItem apk(String path, [int size = 1000]) => RestoreItem(category: 'apks', path: path, size: size);

    test('missing or older apps, newest backed-up version, base.apk first', () {
      final list = appsToReinstall(
        [
          apk('com.example.maps/30/base.apk'),
          apk('com.example.maps/31/split_config.arm64_v8a.apk', 500),
          apk('com.example.maps/31/base.apk', 2000),
          apk('com.example.maps/31/split_config.en.apk', 10),
          apk('com.example.notes/5/base.apk'),
          apk('com.example.same/9/base.apk'),
          apk('com.example.newer/3/base.apk'),
          apk('com.example.splitonly/4/split_config.xx.apk'),
          apk('com.example.weird/base.apk'),
        ],
        {'com.example.maps': 30, 'com.example.same': 9, 'com.example.newer': 4},
        labels: {'com.example.notes': 'Notes', 'com.example.maps': 'Maps'},
      );
      expect([for (final a in list) a.package], ['com.example.maps', 'com.example.notes']);
      final maps = list.first;
      expect(maps.versionCode, 31);
      expect(maps.installedVersion, 30);
      expect(maps.split, isTrue);
      expect(maps.size, 2510);
      expect([for (final f in maps.files) f.path.split('/').last], ['base.apk', 'split_config.arm64_v8a.apk', 'split_config.en.apk']);
      expect(list.last.installedVersion, isNull);
      expect(list.last.split, isFalse);
    });
  });
}
