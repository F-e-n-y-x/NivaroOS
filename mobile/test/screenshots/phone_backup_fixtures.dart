// "Back up this phone" against the fake server: contract-shaped answers of
// the device API (docs/specs/backup-api.json, device_*) for the widget
// tests and the screenshots, and a set-up phone (settings, state, token).
import 'dart:io';

import 'package:nivaroos_mobile/phone_backup/pb_client.dart';
import 'package:nivaroos_mobile/phone_backup/pb_models.dart';
import 'package:nivaroos_mobile/phone_backup/pb_schedule.dart';
import 'package:nivaroos_mobile/phone_backup/pb_service.dart';
import 'package:nivaroos_mobile/phone_backup/pb_store.dart';

import 'harness.dart';

const phoneToken = 'nvd_q3Zp9m1W0yXcVb7aL2kR5tNe8fHs4dJu6gOi1PwQx';
const phoneId = 'dev_4f1c9a2b7e3d5a60';

Map<String, Object?> _env(Object? data, [int status = 200]) => {'success': status, 'message': 'ok', 'data': data};

Map<String, Object?> phoneDevice({bool moving = false}) => {
      'id': phoneId,
      'name': 'Pixel 8',
      'platform': 'android',
      'folder': 'Pixel 8',
      'dest_path': '/DATA/Backup/Pixel 8',
      'last_backup_at': '2026-09-25T01:04:51Z',
      'last_status': 'success',
      'size_bytes': 48318382080,
      'moving': moving,
    };

Map<String, Object?> phoneDestination({bool missing = false, bool usb = false}) => {
      'location': usb || missing ? {'kind': 'usb', 'ref_id': '3A4F-1C22', 'sub_path': 'Phones', 'label': 'Backup Stick'} : null,
      'default': !(usb || missing),
      'path': usb || missing ? '/media/usb-3A4F/Phones/Pixel 8' : '/DATA/Backup/Pixel 8',
      'online': !missing,
      'free_bytes': missing ? null : 812345098240,
      'reserve_bytes': 50000000000,
      if (missing) 'error_code': 'dest_offline',
      'quirks': [],
    };

const _settings = {'keep_last': 10, 'keep_days': 30, 'deleted_purge_days': 0, 'stale_days': 7};

Map<String, Object?> phoneConfig({bool missing = false}) => _env({
      'device': phoneDevice(),
      'destination': phoneDestination(missing: missing),
      'settings': _settings,
      'limits': {'chunk_size': 8388608, 'check_batch': 500},
      'categories': [
        for (final c in PhoneCategory.values) {'category': c.wire},
      ],
    });

Map<String, Object?> phoneDetail({bool missing = false, int snapshots = 14}) => _env({
      'device': phoneDevice(),
      'destination': phoneDestination(missing: missing),
      'settings': _settings,
      'categories': [
        {'category': 'media', 'kind': 'files', 'last_backup_at': '2026-09-25T01:04:51Z', 'last_status': 'success', 'files': 18275, 'deleted_files': 12, 'size_bytes': 48210000000},
        {'category': 'contacts', 'kind': 'export', 'last_backup_at': '2026-09-25T01:04:51Z', 'last_status': 'success', 'exports': 9, 'items': 412, 'size_bytes': 1830000},
        {'category': 'calendar', 'kind': 'export', 'last_backup_at': '2026-09-25T01:04:51Z', 'last_status': 'success', 'exports': 3, 'items': 208, 'size_bytes': 420000},
        {'category': 'sms', 'kind': 'export', 'last_backup_at': '2026-09-25T01:04:51Z', 'last_status': 'success', 'exports': 14, 'items': 5210, 'size_bytes': 9300000},
      ],
      'open_sessions': [],
      'move': null,
      'snapshots': snapshots,
      'excluded': 0,
    });

final Map<String, Object> phoneServer = {
  'GET /v1/backup/devices/$phoneId/config': phoneConfig(),
  'GET /v1/backup/devices/$phoneId': phoneDetail(),
  'PUT /v1/backup/devices/$phoneId/phone-settings': _env({}),
  'GET /v1/backup/devices/$phoneId/snapshots': _env([
    {
      'id': 'snap_3',
      'taken_at': '2026-09-25T01:04:51Z',
      'categories': ['calendar', 'contacts', 'media', 'sms'],
      'status': 'success',
      'counts': {'uploaded': 42, 'bytes': 312456789},
    },
    {
      'id': 'snap_2',
      'taken_at': '2026-09-24T01:03:10Z',
      'categories': ['calendar', 'contacts', 'media', 'sms'],
      'status': 'partial',
      'counts': {'uploaded': 7, 'bytes': 48123000, 'errors': 2},
    },
    {
      'id': 'snap_1',
      'taken_at': '2026-09-23T01:10:00Z',
      'categories': ['media'],
      'status': 'success',
      'counts': {'uploaded': 18230, 'bytes': 47900000000},
    },
  ]),
  'GET /v1/backup/devices/$phoneId/exports': _env([
    {'id': 'exp_1', 'category': 'contacts', 'name': '', 'taken_at': '2026-09-25T01:02:10Z', 'size': 203344, 'items': 412},
    {'id': 'exp_2', 'category': 'calendar', 'name': 'Family', 'taken_at': '2026-09-25T01:02:12Z', 'size': 88120, 'items': 164},
    {'id': 'exp_3', 'category': 'calendar', 'name': 'Work', 'taken_at': '2026-09-25T01:02:12Z', 'size': 20480, 'items': 44},
    {'id': 'exp_4', 'category': 'sms', 'name': '', 'taken_at': '2026-09-25T01:03:30Z', 'size': 18840, 'items': 37},
  ]),
  'GET /v1/backup/devices/$phoneId/snapshots/snap_3/browse': _env({
    'snapshot': 'snap_3',
    'category': 'media',
    'path': '',
    'entries': [
      {'name': 'DCIM', 'path': 'DCIM', 'dir': true},
      {'name': 'Pictures', 'path': 'Pictures', 'dir': true},
      {'name': 'Movies', 'path': 'Movies', 'dir': true},
      {'name': 'IMG_0001.jpg', 'path': 'IMG_0001.jpg', 'size': 2201931, 'mtime': 1758700000000, 'deleted_on_device_at': '2026-09-12T08:00:00Z'},
    ],
    'truncated': false,
  }),
};

/// The drive holding the backups is gone.
final Map<String, Object> phoneDriveMissing = {
  'GET /v1/backup/devices/$phoneId/config': phoneConfig(missing: true),
  'GET /v1/backup/devices/$phoneId': phoneDetail(missing: true),
};

const phoneCredential = DeviceCredential(server: fakeServer, deviceId: phoneId, token: phoneToken, name: 'Pixel 8');

const phoneSettings = PhoneBackupSettings(
  categories: {PhoneCategory.media, PhoneCategory.contacts, PhoneCategory.calendar, PhoneCategory.sms},
  schedule: BackupSchedule(hour: 3),
);

/// A phone backup service over a fresh folder; [state] and [settings]
/// written first (null settings: not set up).
PhoneBackupService phoneService({DeviceCredential? credential = phoneCredential, PhoneBackupSettings? settings = phoneSettings, PhoneBackupState? state}) {
  final dir = Directory.systemTemp.createTempSync('pbshots');
  final store = PhoneBackupStore(dir);
  if (settings != null) store.saveSettings(settings);
  if (state != null) store.saveState(state);
  return PhoneBackupService(credentials: MemoryCredentialStore(credential), store: store, platform: false);
}

final phoneStateOk = PhoneBackupState(
  outcome: RunOutcomes.success,
  message: 'Backed up 42 new items (298 MB).',
  lastRunAt: DateTime.utc(2026, 9, 25, 1, 4, 51),
  lastSuccessAt: DateTime.utc(2026, 9, 25, 1, 4, 51),
  nextRunAt: DateTime(2026, 9, 26, 3),
  categories: {
    'media': CategoryRun(at: DateTime.utc(2026, 9, 25, 1, 4, 51), status: 'success', files: 18275, sent: 40, bytes: 312000000),
    'contacts': CategoryRun(at: DateTime.utc(2026, 9, 25, 1, 4, 51), status: 'success', sent: 1),
    'calendar': CategoryRun(at: DateTime.utc(2026, 9, 25, 1, 4, 51), status: 'success', files: 3),
    'sms': CategoryRun(at: DateTime.utc(2026, 9, 25, 1, 4, 51), status: 'partial', files: 5210, sent: 37, errors: 1),
  },
  errors: const [
    {'category': 'sms', 'path': 'mms 1182', 'message': 'Couldn’t read this message’s attachments'},
  ],
);

PhoneBackupState phoneStateRunning(DateTime now) => phoneStateOk.copyWith(
      running: true,
      heartbeat: now,
      startedAt: now.subtract(const Duration(minutes: 3)),
      progress: const RunProgress(category: 'media', phase: 'Sending', done: 1240, total: 18290, bytes: 86000000, totalBytes: 412000000),
    );

final phoneStateWaiting = phoneStateOk.copyWith(
  outcome: RunOutcomes.waitingDrive,
  message: 'The drive that holds this phone’s backups isn’t connected. The backup waits for it.',
  nextRunAt: DateTime(2026, 9, 25, 15, 3),
  errors: const [],
);
