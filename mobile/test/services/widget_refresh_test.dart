// The "Refresh widgets" setting: what each choice means, that it is saved
// per phone (and kept on sign-out), and how Home's chart history follows
// a new interval.
import 'package:clock/clock.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/dashboard_stats.dart';
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
    test('offers real time, 1 s to 1 min and pull-to-refresh only, 4 s by default', () {
      expect(WidgetRefresh.values.map((r) => r.every), [
        const Duration(seconds: 1), // real time's fallback poll
        const Duration(seconds: 1),
        const Duration(seconds: 2),
        const Duration(seconds: 4),
        const Duration(seconds: 10),
        const Duration(seconds: 30),
        const Duration(minutes: 1),
        null,
      ]);
      expect(WidgetRefresh.defaultValue, WidgetRefresh.s4);
      expect(WidgetRefresh.live.summary, 'Real time');
      expect(WidgetRefresh.s1.summary, 'Every second');
      expect(WidgetRefresh.s4.summary, 'Every 4 seconds');
      expect(WidgetRefresh.m1.summary, 'Every minute');
      expect(WidgetRefresh.manual.summary, 'Only when I pull to refresh');
    });

    test('only the two fastest carry a battery and data note', () {
      expect({for (final r in WidgetRefresh.values) if (r.note != null) r}, {WidgetRefresh.live, WidgetRefresh.s1});
      expect(WidgetRefresh.live.note, contains('battery and data'));
      expect(WidgetRefresh.s1.note, contains('battery and data'));
    });

    test('the console preview follows the choice but never faster than 5 s', () {
      expect(WidgetRefresh.live.previewEvery, const Duration(seconds: 5));
      expect(WidgetRefresh.s1.previewEvery, const Duration(seconds: 5));
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

      for (final r in [WidgetRefresh.live, WidgetRefresh.s1]) {
        await StorageService.instance.setWidgetRefresh(r.name);
        await c.load();
        expect(c.value, r);
      }

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

  group('LiveHistory', () {
    final t0 = DateTime(2026, 10, 8, 12);
    LiveStats at(Duration after, double cpu) => LiveStats(
          stats: DashboardStats.fromUtilization({'cpu': {'percent': cpu}, 'mem': {'total': 100, 'used': 50}}),
          rate: null,
          updatedAt: t0.add(after),
        );

    test('the charts cover two minutes by time, whatever the pace', () {
      for (final pace in [const Duration(milliseconds: 500), const Duration(seconds: 1), const Duration(seconds: 4), const Duration(minutes: 1)]) {
        final h = LiveHistory()..retime(pace);
        for (var t = Duration.zero; t <= const Duration(minutes: 5); t += pace) {
          h.add(at(t, 1));
        }
        expect(h.cpu.times.last.difference(h.cpu.times.first), LiveHistory.span, reason: '$pace');
        expect(h.cpu, hasLength(LiveHistory.span.inMilliseconds ~/ pace.inMilliseconds + 1), reason: '$pace');
        expect(h.window, '2 min');
      }
    });

    test('a change of pace keeps the readings: each is placed by its time', () {
      final h = LiveHistory()..retime(const Duration(seconds: 4));
      for (var i = 0; i < 10; i++) {
        h.add(at(Duration(seconds: 4 * i), i.toDouble()));
      }
      // Retimed just after the last reading, not at the real clock (which
      // would age every reading out once the test runs past t0 + 2 min).
      withClock(Clock.fixed(t0.add(const Duration(seconds: 36))), () => h.retime(const Duration(milliseconds: 500)));
      expect(h.cpu, hasLength(10));
      h.add(at(const Duration(seconds: 36, milliseconds: 500), 10));
      expect(h.cpu, hasLength(11));
      // Two minutes on, the 4 s readings have aged out.
      h.add(at(const Duration(seconds: 156, milliseconds: 500), 11));
      expect(h.cpu, [10, 11]);
    });

    test('readings at the same moment replace each other instead of piling up', () {
      final h = LiveHistory();
      h.add(at(Duration.zero, 1));
      h.add(at(Duration.zero, 2));
      expect(h.cpu, [2]);
    });

    test('pull-to-refresh only counts refreshes, kept from the last interval', () {
      final h = LiveHistory()..retime(const Duration(seconds: 10));
      h.retime(null);
      expect(h.capacity, 13);
      expect(h.window, '12 refreshes');
      for (var i = 0; i < 20; i++) {
        h.add(at(Duration(hours: i), i.toDouble()));
      }
      expect(h.cpu, hasLength(13), reason: 'hours apart, still kept: they count refreshes');
      // Back to an interval: what is older than two minutes goes.
      withClock(Clock.fixed(t0.add(const Duration(hours: 19, minutes: 1))), () => h.retime(const Duration(seconds: 4)));
      expect(h.cpu, [19]);
    });
  });
}
