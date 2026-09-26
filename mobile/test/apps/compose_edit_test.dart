// Edit app: compose file <-> form. The form must write back only what the
// user changed and leave everything else - unknown keys included - exactly
// as the server sent it.
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/models/compose_edit.dart';

String serverYaml() => File('test/screenshots/fixtures/v2/app_management/compose/jellyfin.yaml').readAsStringSync();

Map<String, Object?> serverDoc() => parseComposeYaml(serverYaml());

Map svc(Map doc, String name) => (doc['services'] as Map)[name] as Map;

void main() {
  group('YAML', () {
    test('parses the server file into plain maps', () {
      final doc = serverDoc();
      expect(doc['name'], 'jellyfin');
      expect(svc(doc, 'jellyfin')['ports'], isA<List>());
      expect((svc(doc, 'db')['networks'] as Map)['default'], isNull);
    });

    test('emitted YAML reads back to the same document', () {
      final doc = serverDoc();
      expect(deepEquals(parseComposeYaml(emitYaml(doc)), doc), isTrue);
    });

    test('strings that look like other types stay strings', () {
      final doc = <String, Object?>{
        'name': 'x',
        'services': {
          'a': {
            'image': 'nginx',
            'environment': {
              'A': '8080',
              'B': 'true',
              'C': 'no',
              'D': '',
              'E': 'a: b',
              'F': ' spaced ',
              'G': 'line1\nline2',
              'H': '# not a comment',
              'I': '"quoted" and \\ back',
              'J': '1.5',
              'K': '~',
              'L': null,
              'M': r'$$HOME',
              'N': 'Grüße',
              'O': '-dash',
              'P': 'https://example.com/a?b=c&d',
            },
            'command': ['--foo', 'bar baz'],
            'privileged': true,
            'cpu_shares': 90,
            'x-empty': <String, Object?>{},
            'x-list': <Object?>[],
          },
        },
      };
      final back = parseComposeYaml(emitYaml(doc));
      expect(deepEquals(back, doc), isTrue, reason: emitYaml(doc));
    });

    test('bad YAML and non-compose files are reported', () {
      expect(() => parseComposeYaml('services:\n  a: [\n'), throwsFormatException);
      expect(() => parseComposeYaml('- a\n- b\n'), throwsFormatException);
      expect(() => parseComposeYaml('name: x\n'), throwsFormatException);
      expect(validateComposeText(serverYaml(), 'jellyfin'), isNull);
      expect(validateComposeText(serverYaml().replaceFirst('name: jellyfin', 'name: other'), 'jellyfin'), contains('name: jellyfin'));
      expect(validateComposeText('services: [\n', 'jellyfin'), isNotNull);
    });
  });

  group('form', () {
    test('reads what the web form reads', () {
      final f = ComposeForm.fromDoc(serverDoc());
      expect(f.title, 'Jellyfin');
      expect(f.icon, contains('Jellyfin/icon.svg'));
      expect((f.scheme, f.hostname, f.port, f.index), ('http', '', '8097', '/'));
      expect(f.services.map((s) => s.name), ['jellyfin', 'db']);
      final s = f.services.first;
      expect((s.repo, s.tag, s.digest), ('linuxserver/jellyfin', '10.11.10', null));
      expect(s.restart, 'unless-stopped');
      expect(s.networkMode, 'bridge');
      expect(s.memoryMb, '2048');
      expect(s.caps, 'SYS_ADMIN');
      expect(s.ports.map((p) => p.signature), ['|8097|8096|tcp', '|7359|7359|udp']);
      expect(s.volumes.map((v) => v.signature), ['/DATA/AppData/jellyfin/config|/config|false', '/DATA/Media|/media|true']);
      expect(s.envs.map((e) => e.signature), ['PGID=1000', 'PUID=1000', 'TZ=Europe/London']);
      expect(s.devices.map((d) => d.signature), ['/dev/dri|/dev/dri']);
      expect(s.envHelp['TZ'], 'Your timezone, like Europe/London');
      expect(f.publishedPorts, ['8097']);
      expect(f.link('nivaro.test').toString(), 'http://nivaro.test:8097/');
    });

    test('an unchanged form gives back the exact document', () {
      final doc = serverDoc();
      final out = ComposeForm.fromDoc(doc).applyTo(doc);
      expect(deepEquals(out, doc), isTrue);
      expect(deepEquals(doc, serverDoc()), isTrue, reason: 'applyTo must not touch its input');
    });

    test('rows added empty and removed again change nothing', () {
      final doc = serverDoc();
      final f = ComposeForm.fromDoc(doc);
      f.services.first.ports.add(PortRow());
      f.services.first.envs.add(EnvRow());
      expect(deepEquals(f.applyTo(doc), doc), isTrue);
    });

    test('changing one field changes only that field', () {
      final doc = serverDoc();
      final f = ComposeForm.fromDoc(doc)..services.first.tag = '10.11.11';
      final out = f.applyTo(doc);
      final expected = serverDoc();
      svc(expected, 'jellyfin')['image'] = 'linuxserver/jellyfin:10.11.11';
      expect(deepEquals(out, expected), isTrue);
    });

    test('name, icon and Web UI link go into x-casaos, the rest stays', () {
      final doc = serverDoc();
      final f = ComposeForm.fromDoc(doc)
        ..title = 'Movies'
        ..icon = 'https://example.com/movies.png'
        ..scheme = 'https'
        ..hostname = 'media.example.com'
        ..port = '8920'
        ..index = '/web';
      final x = f.applyTo(doc)['x-casaos'] as Map;
      expect(x['title'], {'custom': 'Movies', 'en_us': 'Movies'});
      expect(x['icon'], 'https://example.com/movies.png');
      expect((x['scheme'], x['hostname'], x['port_map'], x['index']), ('https', 'media.example.com', '8920', '/web'));
      expect(x['store_app_id'], 'jellyfin');
      expect(x['architectures'], ['amd64', 'arm64']);
      expect(f.link('nivaro.test').toString(), 'https://media.example.com:8920/web');
    });

    test('an edited row keeps its extra keys; a new row matches the list style', () {
      final doc = serverDoc();
      final f = ComposeForm.fromDoc(doc);
      final s = f.services.first;
      s.volumes.first.readOnly = true;
      s.volumes.removeAt(1);
      s.volumes.add(VolumeRow(source: '/DATA/Downloads', target: '/downloads'));
      s.ports.first.published = '8098';
      s.ports.add(PortRow(published: '1900', target: '1900', protocol: 'udp'));
      s.envs.firstWhere((e) => e.key == 'TZ').value = 'UTC';
      s.envs.add(EnvRow(key: 'JELLYFIN_PublishedServerUrl', value: 'https://media.example.com'));
      s.devices.clear();
      final out = svc(f.applyTo(doc), 'jellyfin');
      expect(out['volumes'], [
        {
          'type': 'bind',
          'source': '/DATA/AppData/jellyfin/config',
          'target': '/config',
          'bind': {'create_host_path': true},
          'read_only': true,
        },
        {'type': 'bind', 'source': '/DATA/Downloads', 'target': '/downloads'},
      ]);
      expect(out['ports'], [
        {'mode': 'ingress', 'target': 8096, 'published': '8098', 'protocol': 'tcp'},
        {'mode': 'ingress', 'target': 7359, 'published': '7359', 'protocol': 'udp'},
        {'mode': 'ingress', 'target': 1900, 'published': '1900', 'protocol': 'udp'},
      ]);
      expect(out['environment'], {'PGID': '1000', 'PUID': '1000', 'TZ': 'UTC', 'JELLYFIN_PublishedServerUrl': 'https://media.example.com'});
      expect(out.containsKey('devices'), isFalse);
      expect(out['healthcheck'], svc(serverDoc(), 'jellyfin')['healthcheck']);
      expect(out['depends_on'], svc(serverDoc(), 'jellyfin')['depends_on']);
      expect(out['cpu_shares'], 90);
    });

    test('short-syntax lists stay short', () {
      final doc = parseComposeYaml('''
name: web
services:
  web:
    image: ghcr.io/org/web:1.2@sha256:abc
    ports: ["127.0.0.1:8080:80", "53:53/udp"]
    volumes: ["/DATA/web:/data:ro,z"]
    environment: ["A=1", "B"]
    devices: ["/dev/ttyUSB0:/dev/ttyUSB0:rwm"]
    command: ["sh", "-c", "run --flag 'a b'"]
''');
      final f = ComposeForm.fromDoc(doc);
      final s = f.services.single;
      expect((s.repo, s.tag, s.digest), ('ghcr.io/org/web', '1.2', 'sha256:abc'));
      expect(s.ports.first.signature, '127.0.0.1|8080|80|tcp');
      expect(s.volumes.single.readOnly, isTrue);
      expect(s.command, "sh -c 'run --flag '\\''a b'\\'''");
      expect(deepEquals(f.applyTo(doc), doc), isTrue);

      s.ports.first.published = '8081';
      s.ports.add(PortRow(published: '9000', target: '90'));
      s.volumes.single.readOnly = false;
      s.envs.last.value = '2';
      s.devices.single.container = '/dev/serial';
      s.tag = '1.3';
      final out = svc(f.applyTo(doc), 'web');
      expect(out['image'], 'ghcr.io/org/web:1.3', reason: 'a new tag drops the digest pin');
      expect(out['ports'], ['127.0.0.1:8081:80', '53:53/udp', '9000:90']);
      expect(out['volumes'], ['/DATA/web:/data:z']);
      expect(out['environment'], ['A=1', 'B=2']);
      expect(out['devices'], ['/dev/ttyUSB0:/dev/serial:rwm']);
      expect(out['command'], ['sh', '-c', "run --flag 'a b'"], reason: 'untouched command keeps its list form');
    });

    test('network, privileged, caps, limits, command and restart', () {
      final doc = serverDoc();
      final f = ComposeForm.fromDoc(doc);
      final db = f.services[1];
      db
        ..networkMode = 'host'
        ..privileged = true
        ..caps = 'net_admin, SYS_TIME'
        ..cpus = '1.5'
        ..memoryMb = '512'
        ..command = 'postgres -c max_connections=200'
        ..restart = 'always';
      final main = f.services.first
        ..memoryMb = ''
        ..caps = '';
      expect(main.name, 'jellyfin');
      final out = f.applyTo(doc);
      final d = svc(out, 'db');
      expect(d['network_mode'], 'host');
      expect(d.containsKey('networks'), isFalse, reason: 'compose refuses network_mode next to networks');
      expect(d['privileged'], true);
      expect(d['cap_add'], ['NET_ADMIN', 'SYS_TIME']);
      expect(d['deploy'], {
        'resources': {
          'limits': {'cpus': '1.5', 'memory': '512M'},
        },
      });
      expect(d['command'], 'postgres -c max_connections=200');
      expect(d['restart'], 'always');
      final j = svc(out, 'jellyfin');
      expect(j.containsKey('deploy'), isFalse, reason: 'no empty deploy left behind');
      expect(j.containsKey('cap_add'), isFalse);
    });
  });

  group('validation', () {
    test('field rules', () {
      expect(validateTitle(' '), isNotNull);
      expect(validateIconUrl(''), isNull);
      expect(validateIconUrl('https://example.com/a.png'), isNull);
      expect(validateIconUrl('example.com/a.png'), isNotNull);
      expect(validateHost('media.example.com'), isNull);
      expect(validateHost('http://media'), isNotNull);
      expect(validateHost('a/b'), isNotNull);
      expect(validatePort('', required: false), isNull);
      expect(validatePort('', required: true), isNotNull);
      expect(validatePort('8000-8010', required: true), isNull);
      expect(validatePort('70000', required: true), isNotNull);
      expect(validatePort('0', required: true), isNotNull);
      expect(validatePath('web'), isNotNull);
      expect(validatePath('/web'), isNull);
      expect(validateImageRepo(''), isNotNull);
      expect(validateImageRepo('linuxserver/jellyfin'), isNull);
      expect(validateImageRepo('a b'), isNotNull);
      expect(validateTag('10.11.10'), isNull);
      expect(validateTag('bad tag'), isNotNull);
      expect(validateVolumeSource('data'), isNotNull);
      expect(validateVolumeSource('data', named: true), isNull);
      expect(validateContainerPath('config'), isNotNull);
      expect(validateEnvKey('1A'), isNotNull);
      expect(validateEnvKey('TZ'), isNull);
      expect(validateDevice('dri'), isNotNull);
      expect(validateCpus('1.5'), isNull);
      expect(validateCpus('lots'), isNotNull);
      expect(validateMemory('512'), isNull);
      expect(validateMemory('512M'), isNotNull);
      expect(validateCaps('NET_ADMIN, sys_time'), isNull);
      expect(validateCaps('NET-ADMIN'), isNotNull);
    });

    test('problems() finds bad values and duplicates across services', () {
      final f = ComposeForm.fromDoc(serverDoc());
      expect(f.problems(), isEmpty);
      f.title = '';
      f.services[1].ports.add(PortRow(published: '8097', target: '5432'));
      f.services.first.envs.add(EnvRow(key: 'TZ', value: 'UTC'));
      f.services.first.volumes.add(VolumeRow(source: 'relative', target: '/x'));
      final p = f.problems();
      expect(p, contains('Name: Give the app a name'));
      expect(p, contains('Server port 8097/TCP is used twice.'));
      expect(p, contains('jellyfin: TZ is set twice.'));
      expect(p.any((e) => e.startsWith('jellyfin, folder on the server')), isTrue);
    });

    test('secret names', () {
      expect(isSecretName('POSTGRES_PASSWORD'), isTrue);
      expect(isSecretName('API_TOKEN'), isTrue);
      expect(isSecretName('TZ'), isFalse);
    });
  });
}
