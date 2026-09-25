// Screenshots of the VMs area: the list and its states, the create and
// edit forms, the VM console (with its sheets) and the host desktop.
// Same harness and rules as screens_test.dart (brief §7-§8); every shot
// runs strict. PNGs in goldens/vms/.
//
//   flutter test test/screenshots/vms_test.dart --update-goldens
import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/screens/host_desktop_screen.dart';
import 'package:nivaroos_mobile/screens/vm_console_screen.dart';
import 'package:nivaroos_mobile/screens/vm_form_screen.dart';
import 'package:nivaroos_mobile/screens/vm_list_screen.dart';
import 'package:nivaroos_mobile/services/rfb_client.dart';
import 'package:nivaroos_mobile/services/vm_client.dart';
import 'package:nivaroos_mobile/widgets/rfb_view.dart';

import '../vm/vm_fakes.dart';
import 'harness.dart';

const _api = '/v1/vm-sidecar';

Map<String, dynamic> _vmJson(
  String name, {
  String state = 'shutoff',
  int cpus = 2,
  int mem = 2048,
  int disk = 20,
  String bus = 'sata',
  String model = 'e1000e',
  bool usb = false,
  String? iso,
}) =>
    {
      'name': name,
      'state': state,
      'vcpus': cpus,
      'memory_mib': mem,
      'disk_gib': disk,
      'disks': [
        {'path': '/DATA/VMs/$name/$name.qcow2', 'gib': disk, 'bus': bus, 'target': bus == 'virtio' ? 'vda' : 'sda', 'ssd': true}
      ],
      'networks': [
        {'mode': 'bridge', 'bridge_name': 'br0', 'model': model, 'mac': '52:54:00:6e:c7:${name.length.toRadixString(16).padLeft(2, '0')}', 'link_state': 'up'}
      ],
      'iso_path': ?iso,
      if (usb)
        'usb_devices': [
          {'vendor_id': '046d', 'product_id': 'c52b'}
        ],
      'shared_folders': [
        {'source_dir': '/DATA/VMs/share', 'target_tag': 'share'}
      ],
      'firmware': 'uefi',
      'display_width': 1280,
      'display_height': 720,
      'clipboard_channel': true,
    };

final _win11 = _vmJson('win11', state: 'running', cpus: 4, mem: 8192, disk: 64, usb: true);
final _mint = _vmJson('mint', state: 'paused', bus: 'virtio', model: 'virtio', iso: '/DATA/VMs/ISOs/linuxmint-22.3-cinnamon-64bit.iso');

final _mixed = [
  _win11,
  _mint,
  _vmJson('debian-server', cpus: 1, mem: 1024, disk: 16, bus: 'virtio', model: 'virtio'),
  _vmJson('zimaOS'),
];

final _snapshots = [
  {'name': 'before-updates', 'state': 'shutoff', 'creation_time': DateTime(2026, 9, 21, 9, 40).millisecondsSinceEpoch ~/ 1000, 'current': false},
  {'name': 'fresh-install', 'state': 'running', 'creation_time': DateTime(2026, 9, 12, 18, 5).millisecondsSinceEpoch ~/ 1000, 'current': true},
];

late Uint8List _png;

RfbClient _rfb(String? vm, {bool extended = true}) => RfbClient(
      vmName: vm,
      channelFactory: FakeRfbServer(width: 1280, height: 720, extendedClipboard: extended).factory,
    );

RemoteClipboardHistory _history() {
  final h = RemoteClipboardHistory();
  h.record('sudo apt update && sudo apt upgrade -y', ClipDirection.sent, 'win11', now: shotTime.subtract(const Duration(minutes: 12)));
  h.record('https://example.com/download/installer.msi', ClipDirection.copied, 'win11',
      now: shotTime.subtract(const Duration(minutes: 3)));
  return h;
}

Future<void> _tapTooltip(WidgetTester tester, String tooltip) async {
  await tester.tap(find.byTooltip(tooltip).first);
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
}

Future<void> _menu(WidgetTester tester, String item) async {
  await _tapTooltip(tester, 'More options');
  await tester.tap(find.text(item).last);
  for (var i = 0; i < 8; i++) {
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
    await tester.pump(const Duration(milliseconds: 100));
  }
}

/// One VMs-area shot.
class _Shot {
  const _Shot(this.build, {this.overrides = const {}, this.tab = false, this.extra = const {}, this.before, this.dark = true});

  final Widget Function() build;
  final Map<String, Object> overrides;
  final bool tab;

  /// 'small' (360x740) and '2x' (200% text), light and dark.
  final Set<String> extra;
  final Future<void> Function(WidgetTester tester)? before;

  /// Also shoot the dark theme (the consoles are always dark).
  final bool dark;
}

final Map<String, _Shot> _shots = {
  'vm_list': _Shot(
    () => VmListScreen(client: PictureVmClient(_png)),
    tab: true,
    overrides: {'GET $_api/vms': _mixed},
    extra: {'small', '2x'},
  ),
  'vm_list_loading': _Shot(() => VmListScreen(client: _NeverClient()), tab: true),
  'vm_list_empty': _Shot(() => VmListScreen(client: PictureVmClient(null)), tab: true, overrides: {'GET $_api/vms': <Object>[]}),
  'vm_list_error': _Shot(
    () => VmListScreen(client: PictureVmClient(null)),
    tab: true,
    dark: false,
    overrides: {'GET $_api/vms': const FakeResponse({'error': 'vm manager unavailable'}, status: 502)},
  ),
  'vm_list_not_set_up': _Shot(
    () => VmListScreen(client: PictureVmClient(null)),
    tab: true,
    dark: false,
    overrides: {
      'GET $_api/vms': const FakeResponse({'error': 'libvirt: connection refused'}, status: 500),
      'GET $_api/setup/status': {'ready': false, 'missing_packages': ['qemu-system-x86', 'libvirt-daemon-system']},
    },
  ),
  'vm_list_actions': _Shot(
    () => VmListScreen(client: PictureVmClient(_png)),
    tab: true,
    overrides: {'GET $_api/vms': _mixed},
    before: (tester) => _tapTooltip(tester, 'More for win11'),
  ),
  'vm_form_new': _Shot(() => VmFormScreen(client: VmClient(), takenNames: const ['mint']), extra: {'small', '2x'}),
  'vm_form_edit': _Shot(
    () => VmFormScreen(client: VmClient(), existing: Vm.fromJson(_vmJson('zimaOS', usb: true))),
    overrides: {'GET $_api/vms/zimaOS': _vmJson('zimaOS', usb: true)},
    extra: {'2x'},
  ),
  'vm_form_edit_running': _Shot(
    () => VmFormScreen(client: VmClient(), existing: Vm.fromJson(_win11)),
    dark: false,
    overrides: {'GET $_api/vms/win11': _win11},
  ),
  'vm_console': _Shot(
    () => VmConsoleScreen(vmName: 'win11', client: VmClient(), rfb: _rfb('win11'), history: _history()),
    overrides: {'GET $_api/vms/win11': _win11},
    dark: false,
    extra: {'small'},
  ),
  'vm_console_keys': _Shot(
    () => VmConsoleScreen(vmName: 'win11', client: VmClient(), rfb: _rfb('win11'), history: _history()),
    overrides: {'GET $_api/vms/win11': _win11},
    dark: false,
    before: (tester) => _menu(tester, 'Special keys'),
  ),
  'vm_console_clipboard': _Shot(
    () => VmConsoleScreen(vmName: 'win11', client: VmClient(), rfb: _rfb('win11'), history: _history()),
    overrides: {'GET $_api/vms/win11': _win11},
    dark: false,
    extra: {'2x'},
    before: (tester) => _tapTooltip(tester, 'Clipboard'),
  ),
  'vm_console_snapshots': _Shot(
    () => VmConsoleScreen(vmName: 'win11', client: VmClient(), rfb: _rfb('win11'), history: _history()),
    overrides: {'GET $_api/vms/win11': _win11, 'GET $_api/vms/win11/snapshots': _snapshots},
    dark: false,
    before: (tester) => _menu(tester, 'Snapshots'),
  ),
  'vm_console_disc': _Shot(
    () => VmConsoleScreen(vmName: 'mint', client: VmClient(), rfb: _rfb('mint'), history: _history()),
    overrides: {'GET $_api/vms/mint': _mint},
    dark: false,
    before: (tester) => _menu(tester, 'Disc drive'),
  ),
  'vm_console_off': _Shot(() => VmConsoleScreen(vmName: 'mint', client: VmClient(), rfb: _rfb('mint')), dark: false),
  'vm_console_failed': _Shot(
    () => VmConsoleScreen(
      vmName: 'win11',
      client: VmClient(),
      rfb: RfbClient(vmName: 'win11', channelFactory: (uri, headers) async => throw Exception('HTTP 400')),
    ),
    overrides: {'GET $_api/vms/win11': _win11},
    dark: false,
  ),
  'host_desktop': _Shot(() => HostDesktopScreen(client: VmClient(), rfb: _rfb(null, extended: false), history: RemoteClipboardHistory()), dark: false),
  'host_desktop_resolution': _Shot(
    () => HostDesktopScreen(client: VmClient(), rfb: _rfb(null, extended: false), history: RemoteClipboardHistory()),
    dark: false,
    before: (tester) => _menu(tester, 'Screen resolution'),
  ),
  'host_desktop_not_set_up': _Shot(
    () => HostDesktopScreen(client: VmClient(), rfb: _rfb(null)),
    dark: false,
    overrides: {'GET $_api/host/desktop/installed': {'installed': false, 'unit_present': false, 'x11vnc_present': false}},
  ),
};

/// A client whose list never answers: the first-load skeleton.
class _NeverClient extends PictureVmClient {
  _NeverClient() : super(null);
  @override
  Future<List<Vm>> listVms() => Completer<List<Vm>>().future;
}

void main() {
  setUpAll(() async {
    TestWidgetsFlutterBinding.ensureInitialized();
    _png = await fakeDesktopPng(640, 360);
  });
  setUp(() async {
    VmListScreen.clearCache();
    await signIn();
  });

  for (final MapEntry(key: name, value: s) in _shots.entries) {
    Future<void> shootOnce(WidgetTester tester, Brightness b, {Size size = phone, double textScale = 1}) => shoot(
          tester,
          name: name,
          dir: 'vms',
          screen: s.build(),
          brightness: b,
          size: size,
          textScale: textScale,
          overrides: s.overrides,
          tab: s.tab,
          before: s.before,
          pushed: !s.tab,
          // The consoles are always dark whatever the phone's theme; say
          // so in the file name.
          themeName: s.dark ? null : (name.startsWith('vm_console') || name.startsWith('host_desktop') ? 'fixed_dark' : null),
        );
    // Real elevation shadows rather than flutter_test's solid outlines, so
    // the FAB and menus look as they do on a phone; reset before the test
    // ends, as flutter_test requires.
    Future<void> shot(WidgetTester tester, Brightness b, {Size size = phone, double textScale = 1}) async {
      debugDisableShadows = false;
      try {
        await shootOnce(tester, b, size: size, textScale: textScale);
      } finally {
        debugDisableShadows = true;
      }
    }

    for (final b in [Brightness.light, if (s.dark) Brightness.dark]) {
      testWidgets('$name ${b.name}', (tester) => shot(tester, b));
      if (s.extra.contains('small')) testWidgets('$name small ${b.name}', (tester) => shot(tester, b, size: smallPhone));
      if (s.extra.contains('2x')) testWidgets('$name 200% text ${b.name}', (tester) => shot(tester, b, textScale: 2));
    }
  }
}
