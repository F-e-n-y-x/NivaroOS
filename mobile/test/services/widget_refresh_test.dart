// The "Refresh widgets" setting: what each choice means, that it is saved
// per phone (and kept on sign-out), and how Home's chart history follows
// a new interval.
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/services/widget_refresh.dart';
import 'package:nivaroos_mobile/widgets/monitor_modals.dart';

import '../screenshots/harness.dart' show stubPlatformChannels;

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(stubPlatformChannels);

  // StorageService is a process-wide singleton that reads the mock store
  // once, so these tests write through it rather than re-seeding the mock.
  setUpAll(() async {
    FlutterSecureStorage.setMockInitialValues({});
    await StorageService.instance.init();
  });

  group('WidgetRefresh', () {
    test('offers 2 s to 1 min and pull-to-refresh only, 4 s by default', () {
      expect(WidgetRefresh.values.map((r) => r.every), [
        const Duration(seconds: 2),
        const Duration(seconds: 4),
        const Duration(seconds: 10),
        const Duration(seconds: 30),
        const Duration(minutes: 1),
        null,
      ]);
      expect(WidgetRefresh.defaultValue, WidgetRefresh.s4);
      expect(WidgetRefresh.s4.summary, 'Every 4 seconds');
      expect(WidgetRefresh.m1.summary, 'Every minute');
      expect(WidgetRefresh.manual.summary, 'Only when I pull to refresh');
    });

    test('the console preview follows the choice but never faster than 5 s', () {
      expect(WidgetRefresh.s2.previewEvery, const Duration(seconds: 5));
      expect(WidgetRefresh.s4.previewEvery, const Duration(seconds: 5));
      expect(WidgetRefresh.s10.previewEvery, const Duration(seconds: 10));
      expect(WidgetRefresh.m1.previewEvery, const Duration(minutes: 1));
      expect(WidgetRefresh.manual.previewEvery, isNull);
    });
  });

  group('WidgetRefreshController', () {
    test('loads the saved choice, defaulting to 4 s', () async {
      final c = WidgetRefreshController();
      await c.load();
      expect(c.value, WidgetRefresh.s4);

      await StorageService.instance.setWidgetRefresh('s30');
      await c.load();
      expect(c.value, WidgetRefresh.s30);

      await StorageService.instance.setWidgetRefresh('bogus');
      await c.load();
      expect(c.value, WidgetRefresh.s4);
    });

    test('applies a choice at once, saves it, and a new launch reads it back', () async {
      final c = WidgetRefreshController();
      final heard = <WidgetRefresh>[];
      c.addListener(() => heard.add(c.value));
      await c.set(WidgetRefresh.manual);
      expect(heard, [WidgetRefresh.manual]);
      expect(await StorageService.instance.getWidgetRefresh(), 'manual');

      final next = WidgetRefreshController();
      await next.load();
      expect(next.value, WidgetRefresh.manual);

      // The same choice again changes nothing.
      await c.set(WidgetRefresh.manual);
      expect(heard, hasLength(1));
      await c.set(WidgetRefresh.defaultValue);
    });

    test('signing out keeps the choice', () async {
      await StorageService.instance.setWidgetRefresh('s10');
      await StorageService.instance.setSession(accessToken: 't', refreshToken: 'r', username: 'alex');
      await StorageService.instance.clearAll();
      expect(await StorageService.instance.getWidgetRefresh(), 's10');
      expect(await StorageService.instance.getAccessToken(), isNull);
    });
  });

  group('LiveHistory.retime', () {
    test('keeps the charts at two minutes: the reading count follows the interval', () {
      final h = LiveHistory();
      expect(h.capacity, 31);
      expect(h.window, '2 min');
      final counts = {
        for (final r in WidgetRefresh.values.where((r) => r.every != null)) r: (h..retime(r.every)).capacity,
      };
      expect(counts, {WidgetRefresh.s2: 61, WidgetRefresh.s4: 31, WidgetRefresh.s10: 13, WidgetRefresh.s30: 5, WidgetRefresh.m1: 3});
      h.retime(const Duration(seconds: 2));
      expect(h.window, '2 min');
    });

    test('a new interval starts the lines again: old readings would be spaced wrong', () {
      final h = LiveHistory()..retime(const Duration(seconds: 2));
      h.cpu.addAll([for (var i = 0; i < 61; i++) i.toDouble()]);
      h.netDown.addAll([1, 2, 3]);
      // The same interval again (Home re-arming its timer) keeps them.
      h.retime(const Duration(seconds: 2));
      expect(h.cpu, hasLength(61));
      h.retime(const Duration(minutes: 1));
      expect(h.cpu, isEmpty);
      expect(h.netDown, isEmpty);
      expect(h.window, '2 min');
    });

    test('pull-to-refresh only keeps the readings; back to an interval drops them', () {
      final h = LiveHistory()..retime(const Duration(seconds: 4));
      h.cpu.addAll([1, 2, 3]);
      h.retime(null);
      expect(h.cpu, [1, 2, 3]);
      h.retime(const Duration(seconds: 4));
      expect(h.cpu, isEmpty);
    });

    test('pull-to-refresh only keeps the count and labels the chart in refreshes', () {
      final h = LiveHistory()..retime(const Duration(seconds: 10));
      h.retime(null);
      expect(h.capacity, 13);
      expect(h.window, '12 refreshes');
    });
  });
}
