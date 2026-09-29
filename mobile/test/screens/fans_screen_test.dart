// The Fans page against a fake fan API: presets and modes are sent as the
// server expects, a refused change shows the server's reason, read-only
// fans offer no controls, and a server without fan control says so.
import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/fans.dart';
import 'package:nivaroos_mobile/screens/fans/fans_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/fans_api.dart';

import '../screenshots/harness.dart';

Map<String, dynamic> _fixture(String p) => jsonDecode(File('test/screenshots/fixtures/$p.json').readAsStringSync()) as Map<String, dynamic>;

class FakeFansApi implements FansApi {
  FakeFansApi(this.json, {this.failWith, this.statusError});

  Map<String, dynamic> json;
  final ApiException? failWith;
  final ApiException? statusError;
  final calls = <String>[];

  FansStatus get _s => FansStatus.fromJson(json);

  @override
  Future<FansStatus> status() async {
    if (statusError != null) throw statusError!;
    return _s;
  }

  Future<FansStatus> _change(String call) async {
    calls.add(call);
    if (failWith != null) throw failWith!;
    return _s;
  }

  @override
  Future<FansStatus> applyPreset(String preset) => _change('preset $preset');

  @override
  Future<FansStatus> updateFan(String id, Map<String, Object?> changes) => _change('fan $id ${jsonEncode(changes)}');

  @override
  Future<FansStatus> allAuto() => _change('auto');

  @override
  Future<FansStatus> setCritical({int? cpu, int? gpu}) => _change('critical $cpu $gpu');

  @override
  Future<String> identify(String id) async {
    calls.add('identify $id');
    return 'This channel drives the fan reported as fan2.';
  }

  @override
  dynamic noSuchMethod(Invocation i) => super.noSuchMethod(i);
}

Future<void> _pump(WidgetTester tester, Widget w) async {
  tester.view.physicalSize = const Size(412, 1400) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(testApp(w, pushed: true));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 100));
}

Future<void> _done(WidgetTester tester) async {
  await tester.pumpWidget(const SizedBox.shrink());
  await tester.pump(const Duration(seconds: 3));
}

void main() {
  testWidgets('presets are sent to the server', (tester) async {
    final api = FakeFansApi(_fixture('v1/fans/status'));
    final c = FansController(api: api);
    await _pump(tester, FansScreen(controller: c));
    expect(find.text('CPU fan'), findsOneWidget);
    expect(find.text('Custom: fans are set one by one.'), findsOneWidget);
    await tester.tap(find.text('Quiet'));
    await tester.pump();
    expect(api.calls, ['preset quiet']);
    await _done(tester);
    c.dispose();
  });

  testWidgets('a refused change shows the server reason', (tester) async {
    final api = FakeFansApi(_fixture('v1/fans/status'), failWith: ApiException('Fan settings can only be changed by an administrator.', statusCode: 403));
    final c = FansController(api: api);
    await _pump(tester, FanDetailScreen(controller: c, fanId: 'hw-nct6793-nct6775-656-pwm2'));
    await tester.tap(find.text('Fixed'));
    await tester.pump();
    await tester.pump();
    expect(api.calls.single, contains('"mode":"fixed"'));
    expect(find.textContaining('administrator'), findsOneWidget);
    await _done(tester);
    c.dispose();
  });

  testWidgets('a read-only fan shows why and offers no mode', (tester) async {
    final json = _fixture('v1/fans/status');
    final fan = (json['fans'] as List).last as Map<String, dynamic>;
    fan['controllable'] = false;
    fan['state'] = 'readonly';
    fan['readonly_reason'] = 'This NVIDIA driver is too old for fan control (needs driver 520 or newer).';
    final c = FansController(api: FakeFansApi(json));
    await _pump(tester, FanDetailScreen(controller: c, fanId: fan['id'] as String));
    expect(find.textContaining('too old for fan control'), findsOneWidget);
    expect(find.text('Mode'), findsNothing);
    await _done(tester);
    c.dispose();
  });

  testWidgets('a server without fan control says so', (tester) async {
    final c = FansController(api: FakeFansApi(const {}, statusError: ApiException('not found', statusCode: 404)));
    await _pump(tester, FansScreen(controller: c));
    expect(find.text("Fan control isn't available"), findsOneWidget);
    await _done(tester);
    c.dispose();
  });

  testWidgets('identify reports which fan it is', (tester) async {
    final api = FakeFansApi(_fixture('v1/fans/status'));
    final c = FansController(api: api);
    await _pump(tester, FanDetailScreen(controller: c, fanId: 'hw-nct6793-nct6775-656-pwm1'));
    await tester.scrollUntilVisible(find.text('Identify'), 200);
    await tester.tap(find.text('Identify'));
    await tester.pump();
    await tester.pump();
    expect(api.calls, ['identify hw-nct6793-nct6775-656-pwm1']);
    expect(find.textContaining('reported as fan2'), findsWidgets);
    await _done(tester);
    c.dispose();
  });
}
