// The remote session: touches, mouse and keys become the right RFB input
// in touch and mouse pointer mode; the toolbar hides itself; the view
// keeps clear of the keyboard; choices are remembered.
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/rfb_client.dart';
import 'package:nivaroos_mobile/services/storage_service.dart';
import 'package:nivaroos_mobile/widgets/rfb_view.dart';

/// A connected console that records what it would send.
class _Rec extends RfbClient {
  _Rec() : super(channelFactory: (uri, headers) async => throw UnimplementedError()) {
    width = 1280;
    height = 720;
  }
  final pointer = <(int, int, int)>[];
  final keys = <(int, bool)>[];
  bool live = true;
  int connects = 0;
  @override
  bool get isConnected => live;
  @override
  Future<void> connect() async {
    connects++;
    status.value = RfbStatus.connecting;
  }
  @override
  void sendPointer(int x, int y, int buttonMask) => pointer.add((x, y, buttonMask));
  @override
  void sendKey(int keysym, bool down) => keys.add((keysym, down));

  List<int> get masks => pointer.map((p) => p.$3).toList();
}

// 1280x720 logical at ratio 1: fitted, a remote pixel is a view pixel.
void _screen(WidgetTester tester, {Size size = const Size(1280, 720)}) {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}

Future<RfbViewState> _view(WidgetTester tester, _Rec c, RfbInputMode mode) async {
  _screen(tester);
  await tester.pumpWidget(MaterialApp(home: RfbView(client: c, inputMode: mode)));
  return tester.state<RfbViewState>(find.byType(RfbView));
}

Future<void> _twoFingerTap(WidgetTester tester, Offset a, Offset b) async {
  final g1 = await tester.startGesture(a, pointer: 1);
  final g2 = await tester.startGesture(b, pointer: 2);
  await g1.up();
  await g2.up();
}

void main() {
  setUp(() async {
    FlutterSecureStorage.setMockInitialValues({});
    await StorageService.instance.init();
    await StorageService.instance.setConsolePref('input', 'touch');
    await StorageService.instance.setConsolePref('fit@mint', 'fit');
  });

  group('touch mode', () {
    testWidgets('tap clicks where the finger is', (tester) async {
      final c = _Rec();
      await _view(tester, c, RfbInputMode.touch);
      await tester.tapAt(const Offset(100, 200));
      expect(c.pointer, containsAllInOrder([(100, 200, 1), (100, 200, 0)]));
    });

    testWidgets('hold right-clicks there', (tester) async {
      final c = _Rec();
      await _view(tester, c, RfbInputMode.touch);
      final g = await tester.startGesture(const Offset(300, 300));
      await tester.pump(const Duration(milliseconds: 600));
      await g.up();
      expect(c.pointer, containsAllInOrder([(300, 300, 4), (300, 300, 0)]));
      expect(c.masks, isNot(contains(1)));
    });

    testWidgets('drag presses where it starts, drags, releases where it ends', (tester) async {
      final c = _Rec();
      await _view(tester, c, RfbInputMode.touch);
      final g = await tester.startGesture(const Offset(100, 100));
      await g.moveTo(const Offset(150, 120));
      await g.moveTo(const Offset(200, 150));
      await g.up();
      expect(c.pointer, containsAllInOrder([(100, 100, 1), (200, 150, 1), (200, 150, 0)]));
    });

    testWidgets('two-finger tap right-clicks; two-finger double-tap zooms instead', (tester) async {
      final c = _Rec();
      final v = await _view(tester, c, RfbInputMode.touch);
      await _twoFingerTap(tester, const Offset(400, 300), const Offset(500, 300));
      await tester.pump(const Duration(milliseconds: 350));
      expect(c.pointer, contains((450, 300, 4)));

      c.pointer.clear();
      await _twoFingerTap(tester, const Offset(400, 300), const Offset(500, 300));
      await tester.pump(const Duration(milliseconds: 100));
      await _twoFingerTap(tester, const Offset(400, 300), const Offset(500, 300));
      await tester.pump(const Duration(milliseconds: 350));
      expect(c.masks, isNot(contains(4)));
      expect(v.viewport.zoom, 2.5);
    });

    testWidgets('two-finger drag scrolls (fingers up: wheel down); pinch zooms', (tester) async {
      final c = _Rec();
      final v = await _view(tester, c, RfbInputMode.touch);
      final g1 = await tester.startGesture(const Offset(400, 500), pointer: 1);
      final g2 = await tester.startGesture(const Offset(500, 500), pointer: 2);
      for (var y = 490.0; y >= 400; y -= 10) {
        await g1.moveTo(Offset(400, y));
        await g2.moveTo(Offset(500, y));
      }
      await g1.up();
      await g2.up();
      expect(c.masks.where((m) => m == 16).length, greaterThanOrEqualTo(4));
      expect(c.masks, isNot(contains(8)));

      final p1 = await tester.startGesture(const Offset(600, 360), pointer: 3);
      final p2 = await tester.startGesture(const Offset(700, 360), pointer: 4);
      for (var d = 10.0; d <= 100; d += 10) {
        await p1.moveTo(Offset(600 - d, 360));
        await p2.moveTo(Offset(700 + d, 360));
      }
      await p1.up();
      await p2.up();
      await tester.pump();
      expect(v.viewport.zoom, greaterThan(2));
    });
  });

  group('mouse pointer mode', () {
    testWidgets('drag moves the cursor like a trackpad; tap clicks at the cursor, not the finger', (tester) async {
      final c = _Rec();
      final v = await _view(tester, c, RfbInputMode.trackpad);
      final g = await tester.startGesture(const Offset(600, 400));
      for (var x = 620.0; x <= 700; x += 20) {
        await g.moveTo(Offset(x, 400));
      }
      await g.up();
      final x = v.cursor.dx.round(), y = v.cursor.dy.round();
      expect(x, greaterThan(640 + 80), reason: 'moved faster than the finger, from the middle');
      expect(c.masks.toSet(), {0});
      c.pointer.clear();
      await tester.tapAt(const Offset(900, 600));
      expect(c.pointer, containsAllInOrder([(x, y, 1), (x, y, 0)]));
    });

    testWidgets('hold then drag holds the left button down', (tester) async {
      final c = _Rec();
      await _view(tester, c, RfbInputMode.trackpad);
      final g = await tester.startGesture(const Offset(600, 400));
      await tester.pump(const Duration(milliseconds: 600));
      await g.moveTo(const Offset(640, 400));
      await g.moveTo(const Offset(680, 400));
      await g.up();
      expect(c.masks.first, 1);
      expect(c.masks.where((m) => m == 1).length, greaterThanOrEqualTo(3));
      expect(c.masks.last, 0);
    });

    testWidgets('two-finger tap right-clicks at the cursor', (tester) async {
      final c = _Rec();
      await _view(tester, c, RfbInputMode.trackpad);
      await _twoFingerTap(tester, const Offset(400, 300), const Offset(500, 300));
      await tester.pump(const Duration(milliseconds: 350));
      expect(c.pointer, containsAllInOrder([(640, 360, 4), (640, 360, 0)]));
    });
  });

  testWidgets('a real mouse points, clicks with its own buttons and scrolls', (tester) async {
    final c = _Rec();
    await _view(tester, c, RfbInputMode.trackpad);
    final mouse = await tester.createGesture(kind: PointerDeviceKind.mouse, buttons: kSecondaryButton);
    await mouse.addPointer(location: const Offset(300, 200));
    await mouse.moveTo(const Offset(320, 210));
    expect(c.pointer.last, (320, 210, 0));
    await mouse.down(const Offset(320, 210));
    expect(c.pointer.last, (320, 210, 4));
    await mouse.up();
    expect(c.pointer.last, (320, 210, 0));
    await tester.sendEventToBinding(const PointerScrollEvent(position: Offset(320, 210), scrollDelta: Offset(0, 40), kind: PointerDeviceKind.mouse));
    expect(c.masks, containsAllInOrder([16, 0]));
    await mouse.removePointer();
  });

  group('the session', () {
    Future<_Rec> frame(WidgetTester tester, {Size size = const Size(412, 915), bool a11y = false}) async {
      _screen(tester, size: size);
      final c = _Rec();
      await tester.pumpWidget(MaterialApp(
        home: Builder(
          builder: (context) => MediaQuery(
            data: MediaQuery.of(context).copyWith(accessibleNavigation: a11y),
            child: RemoteConsoleFrame(client: c, title: 'mint', clipboardTarget: 'mint'),
          ),
        ),
      ));
      await tester.pump();
      return c;
    }

    testWidgets('the toolbar hides itself after a few seconds and comes back from its tab', (tester) async {
      await frame(tester);
      expect(find.byTooltip('Show keyboard'), findsOneWidget);
      await tester.pump(RemoteConsoleFrameState.toolbarHideAfter + const Duration(seconds: 1));
      await tester.pumpAndSettle();
      expect(find.byTooltip('Show keyboard'), findsNothing);
      expect(find.bySemanticsLabel('Show session toolbar'), findsOneWidget);
      await tester.tap(find.bySemanticsLabel('Show session toolbar'));
      await tester.pumpAndSettle();
      expect(find.byTooltip('Show keyboard'), findsOneWidget);
      // Toolbar targets are at least 48dp.
      expect(tester.getSize(find.byTooltip('Disconnect')).height, greaterThanOrEqualTo(48));
    });

    testWidgets('with TalkBack on, the toolbar stays', (tester) async {
      await frame(tester, a11y: true);
      await tester.pump(RemoteConsoleFrameState.toolbarHideAfter * 2);
      await tester.pumpAndSettle();
      expect(find.byTooltip('Show keyboard'), findsOneWidget);
    });

    testWidgets('the keyboard and the keys row inset the view, which slides the cursor above them', (tester) async {
      await frame(tester);
      RfbView view() => tester.widget<RfbView>(find.byType(RfbView));
      expect(view().bottomInset, 0);
      // Tap low on the desktop so the cursor is where the keyboard will be.
      await tester.tapAt(const Offset(200, 560));
      tester.view.viewInsets = const FakeViewPadding(bottom: 400);
      await tester.pump();
      expect(view().bottomInset, 400);
      await tester.tap(find.byTooltip('Extra keys'));
      await tester.pump();
      expect(view().bottomInset, 400 + RemoteConsoleFrameState.keysRowHeight);
      expect(find.text('Esc'), findsOneWidget);
      final lift = tester.state<RfbViewState>(find.byType(RfbView)).lift;
      expect(lift, closeTo(560 + 32 - (915 - 400 - RemoteConsoleFrameState.keysRowHeight), 1));
    });

    testWidgets('the input mode switches from the toolbar and is remembered', (tester) async {
      await frame(tester);
      expect(tester.widget<RfbView>(find.byType(RfbView)).inputMode, RfbInputMode.touch);
      await tester.tap(find.byTooltip('Input: touch. Switch to mouse pointer'));
      await tester.pump();
      expect(tester.widget<RfbView>(find.byType(RfbView)).inputMode, RfbInputMode.trackpad);
      expect(await StorageService.instance.getConsolePref('input'), 'trackpad');
    });

    testWidgets('the fit is chosen from the Display menu and remembered per machine', (tester) async {
      await frame(tester);
      await tester.tap(find.byTooltip('Display'));
      await tester.pumpAndSettle();
      expect(find.text('Match this phone'), findsNothing, reason: 'a VM screen cannot be resized from here');
      await tester.tap(find.text('Fill screen'));
      await tester.pumpAndSettle();
      expect(tester.widget<RfbView>(find.byType(RfbView)).fit, RfbFit.fill);
      expect(await StorageService.instance.getConsolePref('fit@mint'), 'fill');
    });

    testWidgets('a hardware keyboard sends modifiers and shortcuts as keys', (tester) async {
      final c = await frame(tester);
      await tester.sendKeyDownEvent(LogicalKeyboardKey.controlLeft);
      await tester.sendKeyEvent(LogicalKeyboardKey.keyC);
      await tester.sendKeyUpEvent(LogicalKeyboardKey.controlLeft);
      expect(c.keys, [(Keysym.control, true), (0x63, true), (0x63, false), (Keysym.control, false)]);
      c.keys.clear();
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      expect(c.keys, [(Keysym.escape, true), (Keysym.escape, false), (Keysym.enter, true), (Keysym.enter, false)]);
    });

    testWidgets('a lost connection reconnects by itself over the dimmed picture, then offers Reconnect', (tester) async {
      final c = await frame(tester);
      c.live = false;
      c.status.value = RfbStatus.disconnected;
      await tester.pump();
      expect(find.text('Reconnecting to mint'), findsOneWidget);
      expect(find.text('Try 1 of 3'), findsOneWidget);
      expect(find.byTooltip('Show keyboard'), findsNothing, reason: 'no toolbar while not connected');
      await tester.pump(const Duration(seconds: 1));
      expect(c.connects, 1);
      for (var i = 0; i < 3; i++) {
        c.status.value = RfbStatus.failed;
        await tester.pump(const Duration(seconds: 5));
      }
      expect(c.connects, 3);
      expect(find.text('Connection lost'), findsOneWidget);
      await tester.tap(find.text('Reconnect'));
      expect(c.connects, 4);
      c.live = true;
      c.status.value = RfbStatus.connected;
      await tester.pump();
      expect(find.byTooltip('Show keyboard'), findsOneWidget);
    });

    testWidgets('a latched Ctrl applies to the next key, then lets go', (tester) async {
      final c = await frame(tester);
      await tester.tap(find.byTooltip('Extra keys'));
      await tester.pump();
      await tester.tap(find.text('Ctrl'));
      await tester.tap(find.text('Esc'));
      await tester.pump();
      expect(c.keys, [(Keysym.control, true), (Keysym.escape, true), (Keysym.escape, false), (Keysym.control, false)]);
    });
  });
}
