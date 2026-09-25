// The VM form: saving an edit sends the VM back complete (plan M-04), a
// running VM locks what can't change live, and a new VM's name is checked.
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/screens/vm_form_screen.dart';
import 'package:nivaroos_mobile/services/vm_client.dart';
import 'package:nivaroos_mobile/ui/ui.dart';

import 'vm_fakes.dart';

Map<String, dynamic> _vm({String state = 'shutoff'}) => {
      'name': 'win11',
      'state': state,
      'vcpus': 4,
      'memory_mib': 8192,
      'disks': [
        {'path': '/DATA/VMs/win11/win11.qcow2', 'gib': 64, 'bus': 'sata', 'target': 'sda', 'ssd': true},
        {'path': '/DATA/VMs/win11/data.qcow2', 'gib': 200, 'bus': 'virtio', 'target': 'vda'},
      ],
      'networks': [
        {'mode': 'bridge', 'bridge_name': 'br0', 'model': 'e1000e', 'mac': '52:54:00:aa:bb:01', 'link_state': 'up'},
      ],
      'usb_devices': [
        {'vendor_id': '046d', 'product_id': 'c52b'}
      ],
      'shared_folders': [
        {'source_dir': '/DATA/VMs/share', 'target_tag': 'share'}
      ],
      'firmware': 'uefi',
      'clipboard_channel': true,
    };

class _Server {
  _Server({this.state = 'shutoff'});
  final String state;
  final puts = <Map<String, dynamic>>[];
  final posts = <String>[];

  MockClient get client => MockClient((req) async {
        final path = req.url.path.replaceFirst('/v1/vm-sidecar', '');
        if (req.method == 'GET' && path == '/vms/win11') return http.Response(jsonEncode(_vm(state: state)), 200);
        if (req.method == 'GET' && path == '/isos') return http.Response('[{"name":"virtio-win.iso","size_mib":1500}]', 200);
        if (req.method == 'GET' && path == '/networks') {
          return http.Response('[{"name":"br0","mode":"bridge","host_nic":"enp7s0","active":true}]', 200);
        }
        if (req.method == 'PUT') {
          puts.add(jsonDecode(req.body) as Map<String, dynamic>);
          return http.Response(jsonEncode(_vm()), 200);
        }
        posts.add(path);
        return http.Response('{"error":"unexpected"}', 500);
      });
}

Future<void> _pump(WidgetTester tester, Widget form) async {
  tester.view.physicalSize = const Size(412, 2400) * 3;
  tester.view.devicePixelRatio = 3;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(theme: AppTheme.light(), home: form));
  await tester.pumpAndSettle();
}

void main() {
  testWidgets('saving an edit sends every disk, adapter and device back', (tester) async {
    final server = _Server();
    await http.runWithClient(() async {
      final client = VmClient.withSession(FakeSession());
      await _pump(tester, VmFormScreen(client: client, existing: Vm.fromJson(_vm())));
      expect(find.text('Edit win11'), findsOneWidget);
      expect(find.text('8\u00a0GB'), findsOneWidget);

      await tester.tap(find.byTooltip('More memory'));
      await tester.pump();
      expect(find.text('12\u00a0GB'), findsOneWidget);
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
    }, () => server.client);

    expect(server.puts, hasLength(1));
    final body = server.puts.single;
    expect(body['memory_mib'], 12288);
    expect(body['vcpus'], 4);
    expect([for (final d in body['disks'] as List) (d as Map)['path']],
        ['/DATA/VMs/win11/win11.qcow2', '/DATA/VMs/win11/data.qcow2']);
    expect(((body['networks'] as List).single as Map)['mac'], '52:54:00:aa:bb:01');
    expect(body['usb_devices'], [
      {'vendor_id': '046d', 'product_id': 'c52b'}
    ]);
    expect(body['shared_folders'], isNotEmpty);
    expect(server.posts, isEmpty, reason: 'saving never adds a disk');
  });

  testWidgets("existing disks can't shrink below their size", (tester) async {
    final server = _Server();
    await http.runWithClient(() async {
      await _pump(tester, VmFormScreen(client: VmClient.withSession(FakeSession()), existing: Vm.fromJson(_vm())));
      final shrink = find.byTooltip("Disks can't shrink");
      expect(shrink, findsNWidgets(2));
      final button = tester.widget<IconButton>(find.ancestor(of: shrink.first, matching: find.byType(IconButton)).first);
      expect(button.onPressed, isNull);
      expect(find.text('64 GB'), findsOneWidget);
    }, () => server.client);
  });

  testWidgets('a running VM locks the hardware and says why', (tester) async {
    final server = _Server(state: 'running');
    await http.runWithClient(() async {
      await _pump(tester, VmFormScreen(client: VmClient.withSession(FakeSession()), existing: Vm.fromJson(_vm(state: 'running'))));
      expect(find.textContaining('win11 is running. Shut it down to change CPU'), findsOneWidget);
      final more = tester.widget<IconButton>(find.ancestor(of: find.byTooltip('More CPU cores'), matching: find.byType(IconButton)).first);
      expect(more.onPressed, isNull);
    }, () => server.client);
  });

  testWidgets('a new VM needs a free, valid name', (tester) async {
    final server = _Server();
    await http.runWithClient(() async {
      await _pump(tester, VmFormScreen(client: VmClient.withSession(FakeSession()), takenNames: const ['mint']));
      await tester.tap(find.text('Create'));
      await tester.pumpAndSettle();
      expect(find.text('Give the VM a name'), findsOneWidget);
      expect(find.text('Check the fields marked in red.'), findsOneWidget);

      await tester.enterText(find.widgetWithText(TextFormField, 'Name'), 'Mint');
      await tester.pumpAndSettle();
      expect(find.text('A VM called “Mint” already exists'), findsOneWidget);

      await tester.enterText(find.widgetWithText(TextFormField, 'Name'), 'my vm');
      await tester.pumpAndSettle();
      expect(find.text('Use only letters, numbers, - and _'), findsOneWidget);
    }, () => server.client);
    expect(server.posts, isEmpty);
  });

  testWidgets('the Windows template picks drivers its installer has', (tester) async {
    final server = _Server();
    await http.runWithClient(() async {
      await _pump(tester, VmFormScreen(client: VmClient.withSession(FakeSession())));
      await tester.tap(find.text('Windows'));
      await tester.pumpAndSettle();
      expect(find.text('4\u00a0GB'), findsOneWidget);
      expect(find.text('64 GB'), findsOneWidget);
      final bus = tester.widget<SegmentedButton<String>>(find.byWidgetPredicate(
          (w) => w is SegmentedButton<String> && w.segments.any((s) => s.value == 'sata')));
      expect(bus.selected, {'sata'});
    }, () => server.client);
  });
}
