// RfbClient against an in-memory VNC server: the gateway URL and header
// auth (plan M-02, M-16), frames, keys, and both clipboard forms (WP1-5).
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/services/rfb_client.dart';

import 'vm_fakes.dart';

Future<RfbClient> connected(FakeRfbServer server, {String? vm = 'mint', FakeSession? session}) async {
  final client = RfbClient(vmName: vm, session: session ?? FakeSession(currentToken: 'tok'), channelFactory: server.factory);
  await client.connect();
  return client;
}

/// Lets the fake server's replies and the client's reads run.
Future<void> settle() => Future<void>.delayed(const Duration(milliseconds: 20));

int u32(List<int> b, int at) => ByteData.sublistView(Uint8List.fromList(b), at, at + 4).getUint32(0);
int i32(List<int> b, int at) => ByteData.sublistView(Uint8List.fromList(b), at, at + 4).getInt32(0);

void main() {
  test('connects through the gateway with the token in the header, not the URL', () async {
    final server = FakeRfbServer(sendFrame: false);
    final session = FakeSession(base: 'https://nas.example.com', currentToken: 'tok');
    final client = await connected(server, session: session);
    expect(client.status.value, RfbStatus.connected);
    expect(server.uri.toString(), 'wss://nas.example.com/v1/vm-sidecar/vms/mint/console');
    expect(server.uri!.queryParameters, isEmpty);
    expect(server.headers, {'Authorization': 'tok'});
    expect(session.freshTokenCalls, 1, reason: 'the token is checked for expiry before the handshake');
    expect(client.width, 1280);
    expect(client.height, 720);
    client.dispose();
  });

  test('the host desktop uses /host/console', () async {
    final server = FakeRfbServer(sendFrame: false);
    final client = await connected(server, vm: null);
    expect(server.uri!.path, '/v1/vm-sidecar/host/console');
    client.dispose();
  });

  test('asks for raw, copy-rect, desktop size and the extended clipboard', () async {
    final server = FakeRfbServer(sendFrame: false);
    final client = await connected(server);
    final set = server.messages(2).single;
    final count = (set[2] << 8) | set[3];
    final encodings = [for (var i = 0; i < count; i++) i32(set, 4 + i * 4)];
    expect(encodings, containsAll([0, 1, -223, RfbClient.encodingExtendedClipboard]));
    client.dispose();
  });

  testWidgets('shows the first frame', (tester) async {
    await tester.runAsync(() async {
      final server = FakeRfbServer(width: 64, height: 36);
      final client = await connected(server);
      for (var i = 0; i < 50 && client.frame.value == null; i++) {
        await settle();
      }
      expect(client.frame.value, isNotNull);
      expect(client.frame.value!.width, 64);
      // After a frame it asks for the next, incremental one.
      expect(server.messages(3).where((m) => m[1] == 1), isNotEmpty);
      client.dispose();
    });
  });

  test('keys and pointer go out as RFB messages', () async {
    final server = FakeRfbServer(sendFrame: false);
    final client = await connected(server);
    client.tapKey(0xff0d);
    client.sendPointer(5000, 10, 1);
    final keys = server.messages(4);
    expect(keys.length, 2);
    expect(keys[0][1], 1);
    expect(u32(keys[0], 4), 0xff0d);
    expect(keys[1][1], 0);
    final pointer = server.messages(5).single;
    expect((pointer[2] << 8) | pointer[3], 1279, reason: 'clamped to the screen');
    client.dispose();
  });

  test('typing text sends keysyms, Unicode above Latin-1', () async {
    final server = FakeRfbServer(sendFrame: false);
    final client = await connected(server);
    final n = await client.typeText('a\né');
    expect(n, 3);
    final down = server.messages(4).where((m) => m[1] == 1).map((m) => u32(m, 4)).toList();
    expect(down, [0x61, 0xff0d, 0xe9]);
    expect(RfbClient.keysymForRune('世'.runes.first), 0x01000000 + 0x4e16);
    client.dispose();
  });

  group('clipboard without the extended form', () {
    test('sends Latin-1 cut text and says when characters were lost', () async {
      final server = FakeRfbServer(sendFrame: false);
      final client = await connected(server);
      expect(client.sendClipboard('café'), isTrue);
      expect(client.sendClipboard('日本'), isFalse);
      final cut = server.messages(6);
      expect(i32(cut[0], 4), 4);
      expect(latin1.decode(cut[0].sublist(8)), 'café');
      expect(latin1.decode(cut[1].sublist(8)), '??');
      client.dispose();
    });

    test('reports what the remote copied', () async {
      final server = FakeRfbServer(sendFrame: false);
      final client = await connected(server);
      final copied = client.remoteClipboard.first;
      server.sendCutText('hello');
      expect(await copied, 'hello');
      client.dispose();
    });
  });

  group('extended clipboard (QEMU with the vdagent channel)', () {
    test('answers the caps, then pastes UTF-8 through notify, request, provide', () async {
      final server = FakeRfbServer(sendFrame: false, extendedClipboard: true);
      final client = await connected(server);
      await settle();
      expect(client.extendedClipboard, isTrue);
      final ours = server.messages(6).first;
      expect(i32(ours, 4), lessThan(0), reason: 'extended messages have a negative length');
      expect(u32(ours, 8) & (1 << 24), isNonZero, reason: 'our caps');

      expect(client.sendClipboard('héllo 世界\nline two'), isTrue);
      final notify = server.messages(6).last;
      expect(u32(notify, 8), (1 << 27) | 1);

      server.sendExtendedClipboard((1 << 25) | 1, const []); // request text
      await settle();
      final provide = server.messages(6).last;
      expect(u32(provide, 8), (1 << 28) | 1);
      expect(RfbClient.decodeClipboardProvide(Uint8List.fromList(provide.sublist(12))), 'héllo 世界\nline two');
      client.dispose();
    });

    test('asks for the guest copy and reports it', () async {
      final server = FakeRfbServer(sendFrame: false, extendedClipboard: true);
      final client = await connected(server);
      await settle();
      final copied = client.remoteClipboard.first;
      server.sendExtendedClipboard((1 << 27) | 1, const []); // notify: text available
      await settle();
      expect(u32(server.messages(6).last, 8), (1 << 25) | 1, reason: 'it requests the text');
      server.sendExtendedClipboard((1 << 28) | 1, FakeRfbServer.providePayload('Grüße\n'));
      expect(await copied, 'Grüße\n');
      client.dispose();
    });
  });

  group('failures', () {
    test("a refused connection fails with a reason, and doesn't throw", () async {
      final client = RfbClient(
        vmName: 'mint',
        session: FakeSession(),
        channelFactory: (uri, headers) async => throw Exception('HTTP 400'),
      );
      await client.connect();
      expect(client.status.value, RfbStatus.failed);
      expect(client.error.value, contains('may not be running'));
      client.dispose();
    });

    test('a server error close is reported as such', () async {
      final server = FakeRfbServer(sendFrame: false);
      final client = await connected(server);
      server.channel!.serverClose(1011);
      await settle();
      expect(client.status.value, RfbStatus.disconnected);
      expect(client.error.value, contains('problem on the server'));
      client.dispose();
    });

    test('closing on purpose shows no error, and it can connect again', () async {
      final server = FakeRfbServer(sendFrame: false);
      final client = await connected(server);
      client.close();
      await settle();
      expect(client.status.value, RfbStatus.idle);
      expect(client.error.value, isNull);
      await client.connect();
      expect(client.status.value, RfbStatus.connected);
      client.dispose();
    });
  });
}
