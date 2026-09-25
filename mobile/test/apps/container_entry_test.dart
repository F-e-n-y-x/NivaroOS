import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/container_entry.dart';

void main() {
  group('InstalledApp.statusRequest (plan M-08)', () {
    test('compose apps PUT /compose/{id}/status with a bare string', () {
      const app = InstalledApp(id: 'jellyfin', title: 'Jellyfin', kind: AppKind.compose);
      final r = app.statusRequest(AppAction.stop);
      expect(r.path, '/v2/app_management/compose/jellyfin/status');
      expect(r.body, 'stop');
    });

    test('containers PUT /v1/container/{id}/state with {"state": ...}', () {
      const app = InstalledApp(id: 'searxng', title: 'searxng', kind: AppKind.container);
      final r = app.statusRequest(AppAction.restart);
      expect(r.path, '/v1/container/searxng/state');
      expect(r.body, {'state': 'restart'});
    });

    test('names are path-encoded', () {
      const app = InstalledApp(id: 'my app', title: 'x', kind: AppKind.compose);
      expect(app.statusRequest(AppAction.start).path, '/v2/app_management/compose/my%20app/status');
    });
  });

  group('logsRequest', () {
    test('compose uses lines, containers tail + timestamps', () {
      const c = InstalledApp(id: 'a', title: 'a', kind: AppKind.compose);
      const d = InstalledApp(id: 'b', title: 'b', kind: AppKind.container);
      expect(c.logsRequest(200).query, {'lines': '200'});
      expect(d.logsRequest(500).path, '/v1/container/b/logs');
      expect(d.logsRequest(500).query, {'tail': '500', 'timestamps': 'true'});
    });
  });

  group('address (plan M-13)', () {
    const app = InstalledApp(id: 'j', title: 'J', kind: AppKind.compose, scheme: 'http', port: '8097', index: '/web');

    test('builds scheme://server:port/index on the LAN', () {
      final a = app.address('http://192.168.1.20')!;
      expect(a.url.toString(), 'http://192.168.1.20:8097/web');
      expect(a.lanOnly, isFalse);
    });

    test('marks a port on a public server name as LAN-only', () {
      final a = app.address('https://nas.example.com')!;
      expect(a.url.host, 'nas.example.com');
      expect(a.lanOnly, isTrue);
    });

    test('Tailscale and .local servers are reachable', () {
      expect(app.address('http://100.101.1.2')!.lanOnly, isFalse);
      expect(app.address('https://atom.example-tailnet.ts.net')!.lanOnly, isFalse);
      expect(app.address('http://nas.local')!.lanOnly, isFalse);
    });

    test('an app with its own hostname is not LAN-only', () {
      const own = InstalledApp(id: 'j', title: 'J', kind: AppKind.compose, hostname: 'media.example.com', port: '443', scheme: 'https');
      expect(own.address('https://nas.example.com')!.lanOnly, isFalse);
    });

    test('the user override wins; plain containers have none without it', () {
      const c = InstalledApp(id: 'c', title: 'c', kind: AppKind.container);
      expect(c.address('http://10.0.0.2'), isNull);
      expect(c.copyWith(overrideUrl: 'https://c.example.com').address('http://10.0.0.2')!.url.toString(), 'https://c.example.com');
    });

    test('links read the web UI hostname field', () {
      final l = InstalledApp.fromLink({'name': 'Router', 'hostname': 'http://192.168.1.1'});
      expect(l.address('https://nas.example.com')!.url.toString(), 'http://192.168.1.1');
    });
  });

  test('isLocalHost', () {
    for (final h in ['10.1.2.3', '172.20.0.1', '192.168.0.9', '127.0.0.1', 'nas', 'nas.local', 'box.lan', 'fd00::1']) {
      expect(isLocalHost(h), isTrue, reason: h);
    }
    for (final h in ['nas.example.com', '8.8.8.8', '172.32.0.1', '203.0.113.9']) {
      expect(isLocalHost(h), isFalse, reason: h);
    }
  });

  group('runState', () {
    InstalledApp s(String status) => InstalledApp(id: 'a', title: 'a', kind: AppKind.container, status: status);
    test('maps docker states', () {
      expect(s('running').runState, AppRunState.running);
      expect(s('exited').runState, AppRunState.stopped);
      expect(s('created').runState, AppRunState.stopped);
      expect(s('restarting').runState, AppRunState.restarting);
      expect(s('paused').runState, AppRunState.paused);
      expect(s('weird').runState, AppRunState.unknown);
    });
  });

  test('buildInstalledApps merges containers, links and overrides, sorted', () {
    final apps = buildInstalledApps(
      grid: [
        {'app_type': 'container', 'name': 'nextcloud', 'status': 'running', 'image': 'nextcloud:29', 'title': {'en_us': 'nextcloud'}},
        {'app_type': 'v2app', 'name': 'immich', 'status': 'running', 'port': '2283', 'title': {'en_US': 'Immich'}},
        {'app_type': 'container', 'name': 'nextcloud', 'status': 'running'},
      ],
      containers: [
        {'id': 'abc', 'name': '/nextcloud', 'status': 'Up 3 hours', 'has_update': true, 'auto_update_enabled': false},
      ],
      links: [
        {'name': 'Router', 'hostname': 'http://192.168.1.1'},
      ],
      overrides: {
        'nextcloud': {'title': 'Nextcloud', 'url': 'https://cloud.example.com', 'iconRadius': 20},
      },
    );
    expect(apps.map((a) => a.title), ['Immich', 'Nextcloud', 'Router']);
    final nc = apps[1];
    expect(nc.containerId, 'abc');
    expect(nc.statusText, 'Up 3 hours');
    expect(nc.hasUpdate, isTrue);
    expect(nc.autoUpdate, isFalse);
    expect(nc.overrideUrl, 'https://cloud.example.com');
    expect(nc.iconRadius, 20);
    expect(apps[0].kind, AppKind.compose);
    expect(apps[2].isLink, isTrue);
  });

  group('store', () {
    test('StoreApp reads en_US text, tips, image and architectures', () {
      final a = StoreApp.fromJson('jellyfin', {
        'title': {'en_US': 'Jellyfin'},
        'tagline': {'en_US': 'The personal Media System'},
        'architectures': ['amd64', 'arm64'],
        'main': 'jellyfin',
        'apps': {
          'jellyfin': {'image': 'linuxserver/jellyfin:10.11.10'},
        },
        'tips': {
          'before_install': {'en_US': '  Default password is admin.  '},
        },
        'port_map': '8097',
      });
      expect(a.title, 'Jellyfin');
      expect(a.tagline, 'The personal Media System');
      expect(a.image, 'linuxserver/jellyfin:10.11.10');
      expect(a.tips, 'Default password is admin.');
      expect(a.supports('amd64'), isTrue);
      expect(a.supports('x86_64'), isTrue);
      expect(a.supports('aarch64'), isTrue);
      expect(a.supports('riscv64'), isFalse);
      expect(a.supports(null), isTrue);
    });

    test('empty tips are null', () {
      expect(StoreApp.fromJson('x', {'tips': {'before_install': null}}).tips, isNull);
    });

    test('StoreCatalog parses list and installed, sorted by title', () {
      final c = StoreCatalog.fromResponse({
        'data': {
          'installed': ['b'],
          'list': {
            'b': {'title': {'en_US': 'Beta'}},
            'a': {'title': {'en_US': 'alpha'}},
          },
        },
      });
      expect(c.apps.map((a) => a.id), ['a', 'b']);
      expect(c.installed, {'b'});
    });

    test('StoreCategory drops All and empty categories', () {
      final cats = StoreCategory.listFrom({
        'data': [
          {'name': 'All', 'count': 600},
          {'name': 'Media', 'count': 89},
          {'name': 'BigBear', 'count': 0},
        ],
      });
      expect(cats.map((c) => c.name), ['Media']);
    });
  });

  test('AppEvent reads app-management event properties', () {
    final e = AppEvent.tryParse({
      'sourceID': 'app-management',
      'name': 'app:install-progress',
      'properties': {'app:name': 'jellyfin', 'app:progress': '64'},
    })!;
    expect(e.app, 'jellyfin');
    expect(e.progress, 64);
    expect(AppEvent.tryParse('nope'), isNull);
  });

  test('pickLocale prefers custom, then English variants', () {
    expect(pickLocale({'de_DE': 'x', 'en_US': 'y'}), 'y');
    expect(pickLocale({'custom': 'c', 'en_us': 'y'}), 'c');
    expect(pickLocale({'de_DE': 'x'}), 'x');
    expect(pickLocale(''), isNull);
  });
}
