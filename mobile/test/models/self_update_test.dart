import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/self_update.dart';

void main() {
  test('a newer release on a release install', () {
    final u = SelfUpdate.fromJson({
      'state': {
        'state': 'available',
        'current': '1.4.0',
        'available': {'version': '1.5.0', 'size': 134217728, 'notes_md': '- faster\n'},
      },
      'settings': {'auto': 'install', 'window': '04:00'},
      'running': false,
      'can_rollback': true,
    });
    expect(u.available, '1.5.0');
    expect(u.current, '1.4.0');
    expect(u.notes, '- faster');
    expect(u.busy, isFalse);
    expect(u.canRollback, isTrue);
    expect(u.scheduleText, contains('04:00'));
  });

  test('progress words follow the updater steps', () {
    expect(SelfUpdate.fromJson({'state': {'state': 'downloading', 'progress': 64}}).stepText, 'Downloading (64%)');
    expect(SelfUpdate.fromJson({'state': {'state': 'applying', 'step': 'stopping'}}).stepText, 'Stopping the services that changed');
    final started = SelfUpdate.fromJson({'state': {'state': 'available'}, 'running': true});
    expect(started.busy, isTrue);
    expect(started.stepText, 'Starting');
  });

  test('the last update says what was installed or why it was rolled back', () {
    final r = SelfUpdate.fromJson({
      'state': {
        'state': 'idle',
        'last_result': {'result': 'rolled_back', 'from': '1.4.0', 'to': '1.5.0', 'reason': 'failed: health check'},
      },
    }).result!;
    expect(r.failed, isTrue);
    expect(r.text, '1.5.0 was rolled back to 1.4.0: failed: health check');
    expect(const SelfUpdateResult(result: 'installed', to: '1.5.0').text, 'Updated to 1.5.0');
  });

  test('an old server without the updater parses to nothing on offer', () {
    final u = SelfUpdate.fromJson({});
    expect(u.available, '');
    expect(u.busy, isFalse);
  });
}
