// VmClient: the full-spec save (plan M-04), gateway URLs (M-01), and the
// refresh-and-retry on 401 (M-03).
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/services/api_client.dart';
import 'package:nivaroos_mobile/services/vm_client.dart';

import 'vm_fakes.dart';

/// A VM with everything a careless save could lose: two disks, two
/// adapters, USB and PCI passthrough, the shared folder and a boot order.
final loaded = Vm.fromJson({
  'name': 'win11',
  'state': 'shutoff',
  'vcpus': 4,
  'memory_mib': 8192,
  'disk_gib': 64,
  'disks': [
    {'path': '/DATA/VMs/win11/win11.qcow2', 'gib': 64, 'bus': 'sata', 'target': 'sda', 'ssd': true},
    {'path': '/DATA/VMs/win11/win11-disk2.qcow2', 'gib': 200, 'bus': 'virtio', 'target': 'vda'},
  ],
  'networks': [
    {'mode': 'bridge', 'bridge_name': 'br0', 'model': 'e1000e', 'mac': '52:54:00:aa:bb:01', 'link_state': 'up'},
    {'mode': 'nat', 'model': 'virtio', 'mac': '52:54:00:aa:bb:02', 'link_state': 'down'},
  ],
  'iso_path': '/DATA/VMs/ISOs/virtio-win.iso',
  'usb_devices': [
    {'vendor_id': '046d', 'product_id': 'c52b'}
  ],
  'pci_devices': [
    {'address': '0000:01:00.0'}
  ],
  'shared_folders': [
    {'source_dir': '/DATA/VMs/share', 'target_tag': 'share', 'read_only': true}
  ],
  'boot_order': ['sda', 'cdrom'],
  'firmware': 'uefi',
  'display_width': 1920,
  'display_height': 1080,
  'clipboard_channel': true,
});

void main() {
  group('buildUpdateBody (M-04)', () {
    test('changing memory sends everything else back exactly as it was', () {
      final body = VmClient.buildUpdateBody(loaded, const VmEdits(memoryMib: 16384));
      expect(body['memory_mib'], 16384);
      expect(body['vcpus'], 4);
      expect(body['firmware'], 'uefi');
      expect(body['disks'], [
        {'path': '/DATA/VMs/win11/win11.qcow2', 'gib': 64, 'bus': 'sata', 'target': 'sda', 'ssd': true},
        {'path': '/DATA/VMs/win11/win11-disk2.qcow2', 'gib': 200, 'bus': 'virtio', 'target': 'vda', 'ssd': false},
      ]);
      expect(body['networks'], [
        {'mode': 'bridge', 'bridge_name': 'br0', 'model': 'e1000e', 'mac': '52:54:00:aa:bb:01', 'link_state': 'up'},
        {'mode': 'nat', 'model': 'virtio', 'mac': '52:54:00:aa:bb:02', 'link_state': 'down'},
      ]);
      expect(body['usb_devices'], [
        {'vendor_id': '046d', 'product_id': 'c52b'}
      ]);
      expect(body['pci_devices'], [
        {'address': '0000:01:00.0'}
      ]);
      expect(body['shared_folders'], [
        {'source_dir': '/DATA/VMs/share', 'target_tag': 'share', 'read_only': true}
      ]);
      expect(body['boot_order'], ['sda', 'cdrom']);
      expect(body['iso_path'], '/DATA/VMs/ISOs/virtio-win.iso');
      expect(body['display_width'], 1920);
      expect(body['display_height'], 1080);
    });

    test('never asks for a new disk or a new adapter', () {
      final body = VmClient.buildUpdateBody(loaded, const VmEdits(vcpus: 2));
      for (final d in body['disks'] as List) {
        expect((d as Map)['path'], isNotEmpty, reason: 'a disk without its path is provisioned as a new disk');
      }
      for (final n in body['networks'] as List) {
        expect((n as Map)['mac'], isNotEmpty, reason: 'an adapter without its MAC is a new adapter');
      }
      expect((body['disks'] as List).length, loaded.disks.length);
      expect((body['networks'] as List).length, loaded.networks.length);
    });

    test('a round trip of the unchanged VM is the same spec', () {
      final body = VmClient.buildUpdateBody(loaded, const VmEdits());
      final again = VmClient.buildUpdateBody(loaded, VmEdits(displayWidth: loaded.displayWidth, displayHeight: loaded.displayHeight));
      expect(jsonEncode(again), jsonEncode(body));
    });

    test('a disk can grow by position, never shrink', () {
      final body = VmClient.buildUpdateBody(loaded, const VmEdits(diskSizes: {1: 256}));
      expect(((body['disks'] as List)[1] as Map)['gib'], 256);
      expect(((body['disks'] as List)[1] as Map)['path'], '/DATA/VMs/win11/win11-disk2.qcow2');
      expect(((body['disks'] as List)[0] as Map)['gib'], 64);
      expect(() => VmClient.buildUpdateBody(loaded, const VmEdits(diskSizes: {0: 32})), throwsArgumentError);
    });

    test('editing an adapter keeps its MAC and link state', () {
      final body = VmClient.buildUpdateBody(
        loaded,
        const VmEdits(networks: {1: VmNetwork(mode: 'bridge', bridgeName: 'br1', model: 'e1000e', mac: 'ignored')}),
      );
      expect((body['networks'] as List)[1], {
        'mode': 'bridge',
        'bridge_name': 'br1',
        'model': 'e1000e',
        'mac': '52:54:00:aa:bb:02',
        'link_state': 'down',
      });
    });

    test('ejecting the ISO and clearing the resolution leave them out', () {
      final body = VmClient.buildUpdateBody(loaded, const VmEdits(isoPath: '', displayWidth: 0, displayHeight: 0));
      expect(body.containsKey('iso_path'), isFalse);
      expect(body.containsKey('display_width'), isFalse);
    });

    test('a VM with no shared folders or boot order leaves them to the server', () {
      final bare = Vm.fromJson({'name': 'x', 'state': 'shutoff', 'vcpus': 1, 'memory_mib': 512});
      final body = VmClient.buildUpdateBody(bare, const VmEdits());
      expect(body.containsKey('shared_folders'), isFalse, reason: 'null keeps the current ones; [] would remove them');
      expect(body.containsKey('boot_order'), isFalse);
      expect(body['usb_devices'], isEmpty);
    });
  });

  group('Vm.fromJson', () {
    test('reads every field the sidecar sends (M-05)', () {
      expect(loaded.usbDevices.single.vendorId, '046d');
      expect(loaded.pciDevices.single.address, '0000:01:00.0');
      expect(loaded.sharedFolders.single.readOnly, isTrue);
      expect(loaded.bootOrder, ['sda', 'cdrom']);
      expect(loaded.clipboardChannel, isTrue);
      expect(loaded.disks.first.target, 'sda');
      expect(loaded.totalDiskGib, 264);
    });

    test('knows paused and crashed apart from off', () {
      expect(Vm.fromJson({'state': 'paused'}).powerState, VmPowerState.paused);
      expect(Vm.fromJson({'state': 'paused'}).isActive, isTrue);
      expect(Vm.fromJson({'state': 'paused'}).isRunning, isFalse);
      expect(Vm.fromJson({'state': 'crashed'}).powerState, VmPowerState.crashed);
      expect(Vm.fromJson({'state': 'shutoff'}).powerState, VmPowerState.stopped);
    });

    test('an ISO listed by name gets its full path', () {
      final iso = VmIso.fromJson({'name': 'debian.iso', 'size_mib': 756});
      expect(iso.path, '/DATA/VMs/ISOs/debian.iso');
      expect(iso.sizeMib, 756);
    });
  });

  group('requests', () {
    test('go to the gateway route with the token as a header, never to :28641', () async {
      final seen = <http.Request>[];
      final client = VmClient.withSession(FakeSession(base: 'https://nas.example.com', currentToken: 'tok'));
      await http.runWithClient(() async {
        await client.updateVm(loaded, const VmEdits(memoryMib: 4096));
      }, () => MockClient((req) async {
            seen.add(req);
            return http.Response(jsonEncode({'name': 'win11', 'state': 'shutoff'}), 200);
          }));
      final req = seen.single;
      expect(req.method, 'PUT');
      expect(req.url.toString(), 'https://nas.example.com/v1/vm-sidecar/vms/win11');
      expect(req.url.port, 443);
      expect(req.headers['Authorization'], 'tok');
      expect(req.url.queryParameters.containsKey('token'), isFalse);
      final body = jsonDecode(req.body) as Map<String, dynamic>;
      expect(body['memory_mib'], 4096);
      expect((body['disks'] as List).length, 2);
    });

    test('the default session builds URLs from the dashboard address', () {
      ApiClient.instance.setBaseUrl('https://nas.example.com/');
      const s = VmSession();
      expect(s.uri('/vms').toString(), 'https://nas.example.com/v1/vm-sidecar/vms');
      expect(s.webSocketUri('/vms/mint/console').toString(), 'wss://nas.example.com/v1/vm-sidecar/vms/mint/console');
      ApiClient.instance.setBaseUrl('http://192.168.1.20:8080');
      expect(s.webSocketUri('/host/console').toString(), 'ws://192.168.1.20:8080/v1/vm-sidecar/host/console');
    });

    test('a 401 refreshes the token once and retries', () async {
      final session = FakeSession(currentToken: 'old', refreshTo: 'new');
      final tokens = <String?>[];
      final vms = await http.runWithClient(() => VmClient.withSession(session).listVms(), () => MockClient((req) async {
            tokens.add(req.headers['Authorization']);
            return req.headers['Authorization'] == 'new'
                ? http.Response('[{"name":"mint","state":"running"}]', 200)
                : http.Response('{"success":401,"message":"token is invalid"}', 401);
          }));
      expect(tokens, ['old', 'new']);
      expect(session.refreshes, 1);
      expect(vms.single.name, 'mint');
    });

    test('a 401 that the refresh cannot fix is reported as signed out', () async {
      final session = FakeSession(currentToken: 'old');
      final future = http.runWithClient(() => VmClient.withSession(session).listVms(),
          () => MockClient((req) async => http.Response('{"message":"token is invalid"}', 401)));
      await expectLater(future, throwsA(isA<VmException>().having((e) => e.kind, 'kind', VmErrorKind.unauthorized)));
    });

    test('errors are sorted by kind with the sidecar message', () async {
      Future<VmException> fail(http.Response res) async {
        try {
          await http.runWithClient(() => VmClient.withSession(FakeSession()).getVm('mint'), () => MockClient((_) async => res));
        } on VmException catch (e) {
          return e;
        }
        throw StateError('no error');
      }

      final gone = await fail(http.Response('{"error":"vm \\"mint\\" not found"}', 404));
      expect(gone.kind, VmErrorKind.notFound);
      expect(gone.message, 'VM "mint" not found.');
      expect((await fail(http.Response('{"error":"vm manager unavailable"}', 502))).kind, VmErrorKind.unavailable);
      expect((await fail(http.Response('{"error":"is running"}', 409))).kind, VmErrorKind.conflict);
    });

    test('no network is offline', () async {
      final future = http.runWithClient(() => VmClient.withSession(FakeSession()).listVms(),
          () => MockClient((_) async => throw const SocketException('no route')));
      await expectLater(future, throwsA(isA<VmException>().having((e) => e.kind, 'kind', VmErrorKind.offline)));
    });

    test('adding a disk is its own call', () async {
      late http.Request seen;
      await http.runWithClient(
          () => VmClient.withSession(FakeSession()).addDisk('win11', gib: 32),
          () => MockClient((req) async {
                seen = req;
                return http.Response('{"path":"/DATA/VMs/win11/win11-disk3.qcow2","gib":32,"bus":"virtio","target":"vdb"}', 201);
              }));
      expect(seen.method, 'POST');
      expect(seen.url.path, '/v1/vm-sidecar/vms/win11/disks');
      expect(jsonDecode(seen.body), {'gib': 32, 'bus': 'virtio', 'ssd': true});
    });
  });
}
