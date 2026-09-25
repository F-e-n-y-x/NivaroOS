import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nivaroos_mobile/models/container_entry.dart';
import 'package:nivaroos_mobile/screens/app_store_screen.dart';
import 'package:nivaroos_mobile/screens/apps_screen.dart';
import 'package:nivaroos_mobile/services/api_client.dart';

void main() {
  setUp(() {
    ApiClient.instance.setBaseUrl('http://nivaro.test');
    ApiClient.instance.setSession('t', 'r');
  });

  test('compose start/stop uses PUT /status with a bare JSON string, then polls (M-08)', () async {
    final calls = <String>[];
    var polls = 0;
    final client = MockClient((req) async {
      calls.add('${req.method} ${req.url.path} ${req.body}');
      if (req.method == 'PUT') return http.Response(jsonEncode({'message': 'ok'}), 200);
      polls++;
      return http.Response(jsonEncode({'data': {'status': polls < 2 ? 'running' : 'exited'}}), 200);
    });
    const app = InstalledApp(id: 'jellyfin', title: 'Jellyfin', kind: AppKind.compose, status: 'running');
    final state = await http.runWithClient(
      () => AppsApi.setStatus(app, AppAction.stop, interval: Duration.zero),
      () => client,
    );
    expect(calls.first, 'PUT /v2/app_management/compose/jellyfin/status "stop"');
    expect(calls.skip(1).every((c) => c.startsWith('GET /v2/app_management/compose/jellyfin')), isTrue);
    expect(state, 'exited');
  });

  test('container restart uses PUT /v1/container/{id}/state and takes the answer', () async {
    final calls = <String>[];
    final client = MockClient((req) async {
      calls.add('${req.method} ${req.url.path} ${req.body}');
      return http.Response(jsonEncode({'success': 200, 'data': 'running'}), 200);
    });
    const app = InstalledApp(id: 'searxng', title: 'searxng', kind: AppKind.container, status: 'exited');
    final state = await http.runWithClient(() => AppsApi.setStatus(app, AppAction.restart), () => client);
    expect(calls, ['PUT /v1/container/searxng/state {"state":"restart"}']);
    expect(state, 'running');
  });

  test('update: compose PATCHes /compose/{id}, a container POSTs /v1/container/{id}/update', () async {
    final calls = <String>[];
    final client = MockClient((req) async {
      calls.add('${req.method} ${req.url.path}');
      return http.Response(jsonEncode({'message': 'ok'}), 200);
    });
    await http.runWithClient(() async {
      await AppsApi.startUpdate(const InstalledApp(id: 'jellyfin', title: 'Jellyfin', kind: AppKind.compose));
      await AppsApi.startUpdate(const InstalledApp(id: 'searxng', title: 's', kind: AppKind.container));
    }, () => client);
    expect(calls, ['PATCH /v2/app_management/compose/jellyfin', 'POST /v1/container/searxng/update']);
  });

  test('uninstall: compose by query, container by body', () async {
    final calls = <String>[];
    final client = MockClient((req) async {
      calls.add('${req.method} ${req.url} ${req.body}');
      return http.Response(jsonEncode({'message': 'ok'}), 200);
    });
    await http.runWithClient(() async {
      await AppsApi.uninstall(const InstalledApp(id: 'immich', title: 'Immich', kind: AppKind.compose), deleteData: true);
      await AppsApi.uninstall(const InstalledApp(id: 'searxng', title: 's', kind: AppKind.container), deleteData: false);
    }, () => client);
    expect(calls[0], 'DELETE http://nivaro.test/v2/app_management/compose/immich?delete_config_folder=true ');
    expect(calls[1], 'DELETE http://nivaro.test/v1/container/searxng {"delete_config_folder":false}');
  });

  test('store install fetches the compose YAML and posts it as YAML (no fallback list, M-31)', () async {
    final calls = <String>[];
    final client = MockClient((req) async {
      calls.add('${req.method} ${req.url.path}?${req.url.query} ${req.headers['Content-Type'] ?? req.headers['content-type'] ?? ''} ${req.headers['Accept'] ?? ''}');
      if (req.method == 'GET') return http.Response('name: jellyfin\nservices: {}\n', 200);
      return http.Response(jsonEncode({'message': 'ok'}), 200);
    });
    await http.runWithClient(() => StoreInstaller.instance.install('jellyfin'), () => client);
    expect(calls[0], startsWith('GET /v2/app_management/apps/jellyfin/compose?'));
    expect(calls[0], endsWith('application/yaml'));
    expect(calls[1], startsWith('POST /v2/app_management/compose?check_port_conflict=true application/yaml'));
    expect(StoreInstaller.instance.progressOf('jellyfin')!.running, isTrue);

    StoreInstaller.instance.handleEvent(const AppEvent('app:install-progress', {'app:name': 'jellyfin', 'app:progress': '40'}));
    expect(StoreInstaller.instance.progressOf('jellyfin')!.percent, 40);
    StoreInstaller.instance.handleEvent(const AppEvent('app:install-end', {'app:name': 'jellyfin'}));
    expect(StoreInstaller.instance.progressOf('jellyfin')!.done, isTrue);
    StoreInstaller.instance.debugSet('jellyfin', null);
  });

  test('a refused install is reported and forgotten', () async {
    final client = MockClient((req) async => req.method == 'GET'
        ? http.Response('services: {}', 200)
        : http.Response(jsonEncode({'message': 'port 8097 is already in use'}), 400));
    await expectLater(http.runWithClient(() => StoreInstaller.instance.install('jellyfin'), () => client), throwsA(isA<ApiException>()));
    expect(StoreInstaller.instance.progressOf('jellyfin'), isNull);
  });
}
