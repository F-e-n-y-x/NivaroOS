// Edit app: an installed compose app's settings as a form, and back.
//
// The server hands out the app's compose file as YAML
// (`GET /v2/app_management/compose/{id}` with `Accept: application/yaml`,
// services/app-management route/v2/compose_app.go MyComposeApp) and takes
// the whole file back on `PUT /v2/app_management/compose/{id}`
// (ApplyComposeAppSettings), which saves it, pulls the images and recreates
// the containers. The web dashboard's installer form does the same
// (ui/src/apps/app-store/composeForm.js).
//
// Like the web form, the form here is only a view of the document:
// [ComposeForm.fromDoc] reads the fields it shows, and
// [ComposeForm.applyTo] writes the fields that changed back into a copy of
// the same document. Everything else - other keys, labels, healthchecks,
// depends_on, the store's x-casaos metadata - is left exactly as it was,
// and a field the user didn't touch keeps its original shape (a list
// command stays a list, a byte memory limit stays bytes).
import 'package:yaml/yaml.dart';

import 'container_entry.dart' show pickLocale;

// ---------------------------------------------------------------------------
// YAML in and out
// ---------------------------------------------------------------------------

/// Parses a compose file into plain maps and lists. Throws a
/// [FormatException] with a readable message when it isn't a compose file.
Map<String, Object?> parseComposeYaml(String text) {
  Object? doc;
  try {
    doc = loadYaml(text);
  } on YamlException catch (e) {
    final span = e.span;
    final where = span == null ? '' : ' (line ${span.start.line + 1})';
    throw FormatException('${e.message}$where');
  }
  final plain = _plain(doc);
  if (plain is! Map<String, Object?>) throw const FormatException('This is not a compose file: it should start with keys like name: and services:.');
  final services = plain['services'];
  if (services is! Map || services.isEmpty) throw const FormatException('The file has no services: section.');
  for (final e in services.entries) {
    if (e.value is! Map) throw FormatException('The service “${e.key}” has no settings.');
  }
  return plain;
}

Object? _plain(Object? node) {
  if (node is YamlMap) return <String, Object?>{for (final e in node.nodes.entries) '${(e.key as YamlNode).value}': _plain(e.value.value)};
  if (node is YamlList) return [for (final n in node.nodes) _plain(n.value)];
  if (node is Map) return <String, Object?>{for (final e in node.entries) '${e.key}': _plain(e.value)};
  if (node is List) return [for (final v in node) _plain(v)];
  return node;
}

/// A deep copy of plain maps, lists and scalars.
Object? deepCopy(Object? v) {
  if (v is Map) return <String, Object?>{for (final e in v.entries) '${e.key}': deepCopy(e.value)};
  if (v is List) return [for (final x in v) deepCopy(x)];
  return v;
}

/// Structural equality of plain maps, lists and scalars (map order ignored).
bool deepEquals(Object? a, Object? b) {
  if (a is Map && b is Map) {
    if (a.length != b.length) return false;
    for (final k in a.keys) {
      if (!b.containsKey(k) || !deepEquals(a[k], b[k])) return false;
    }
    return true;
  }
  if (a is List && b is List) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (!deepEquals(a[i], b[i])) return false;
    }
    return true;
  }
  return a == b;
}

/// Writes plain maps, lists and scalars as block-style YAML that
/// [parseComposeYaml] (and the server) reads back to the same values.
String emitYaml(Object? value) {
  final out = StringBuffer();
  if (value is Map && value.isNotEmpty) {
    _emitMap(out, value, 0);
  } else if (value is List && value.isNotEmpty) {
    _emitList(out, value, 0);
  } else {
    out.writeln(_scalar(value));
  }
  return out.toString();
}

void _emitMap(StringBuffer out, Map map, int indent, {bool firstInline = false}) {
  var first = true;
  for (final e in map.entries) {
    final pad = firstInline && first ? '' : ' ' * indent;
    first = false;
    final key = _key('${e.key}');
    final v = e.value;
    if (v is Map && v.isNotEmpty) {
      out.writeln('$pad$key:');
      _emitMap(out, v, indent + 2);
    } else if (v is List && v.isNotEmpty) {
      out.writeln('$pad$key:');
      _emitList(out, v, indent + 2);
    } else {
      out.writeln('$pad$key: ${_scalar(v)}');
    }
  }
}

void _emitList(StringBuffer out, List list, int indent) {
  final pad = ' ' * indent;
  for (final v in list) {
    if (v is Map && v.isNotEmpty) {
      out.write('$pad- ');
      _emitMap(out, v, indent + 2, firstInline: true);
    } else if (v is List && v.isNotEmpty) {
      out.writeln('$pad-');
      _emitList(out, v, indent + 2);
    } else {
      out.writeln('$pad- ${_scalar(v)}');
    }
  }
}

String _key(String k) => _plainSafe(k) ? k : _quoted(k);

String _scalar(Object? v) {
  if (v == null) return 'null';
  if (v is bool || v is int) return '$v';
  if (v is double) return v.isFinite ? '$v' : (v.isNaN ? '.nan' : (v > 0 ? '.inf' : '-.inf'));
  if (v is Map) return '{}';
  if (v is List) return '[]';
  final s = '$v';
  return _plainSafe(s) ? s : _quoted(s);
}

final _plainRe = RegExp(r'^[A-Za-z_/][A-Za-z0-9_./@%+=,~-]*(?::[A-Za-z0-9_./@%+=,~-]+)*$');
final _reserved = RegExp(r'^(true|false|yes|no|on|off|y|n|null|~)$', caseSensitive: false);

// A string that reads back as the same string when written bare.
bool _plainSafe(String s) => s.isNotEmpty && _plainRe.hasMatch(s) && !_reserved.hasMatch(s);

String _quoted(String s) {
  final b = StringBuffer('"');
  for (final r in s.runes) {
    switch (r) {
      case 0x22:
        b.write(r'\"');
      case 0x5C:
        b.write(r'\\');
      case 0x0A:
        b.write(r'\n');
      case 0x09:
        b.write(r'\t');
      case 0x0D:
        b.write(r'\r');
      default:
        if (r < 0x20 || r == 0x7F) {
          b.write('\\u${r.toRadixString(16).padLeft(4, '0')}');
        } else {
          b.writeCharCode(r);
        }
    }
  }
  b.write('"');
  return b.toString();
}

// ---------------------------------------------------------------------------
// Values the form shows
// ---------------------------------------------------------------------------

/// A Docker image reference split for editing: `ghcr.io/org/app:1.2@sha256:…`
/// is repository `ghcr.io/org/app`, tag `1.2` and a digest.
typedef ImageRef = ({String repo, String tag, String? digest});

ImageRef splitImage(String image) {
  var ref = image.trim();
  String? digest;
  final at = ref.indexOf('@');
  if (at >= 0) {
    digest = ref.substring(at + 1);
    ref = ref.substring(0, at);
  }
  final colon = ref.lastIndexOf(':');
  if (colon > ref.lastIndexOf('/')) return (repo: ref.substring(0, colon), tag: ref.substring(colon + 1), digest: digest);
  return (repo: ref, tag: '', digest: digest);
}

String joinImage(String repo, String tag, [String? digest]) => '${repo.trim()}${tag.trim().isEmpty ? '' : ':${tag.trim()}'}${digest == null ? '' : '@$digest'}';

/// Compose memory limits come back from the server in bytes; the form
/// shows megabytes (like the web form's memoryToMB).
String memoryToMb(Object? m) {
  if (m == null || '$m'.trim().isEmpty) return '';
  final s = '$m'.trim();
  if (RegExp(r'^\d+$').hasMatch(s)) {
    final bytes = int.parse(s);
    return bytes >= 1024 * 1024 ? '${(bytes / (1024 * 1024)).round()}' : s;
  }
  final mm = RegExp(r'^(\d+(?:\.\d+)?)\s*([kKmMgG])[bB]?$').firstMatch(s);
  if (mm == null) return s;
  final n = double.parse(mm.group(1)!);
  final unit = mm.group(2)!.toLowerCase();
  return '${(unit == 'g'
      ? n * 1024
      : unit == 'k'
      ? n / 1024
      : n).round()}';
}

/// A list command shown shell-quoted, so it reads as typed and round-trips.
String commandToText(Object? c) {
  if (c == null) return '';
  if (c is! List) return '$c';
  return c
      .map((a) {
        final s = '$a';
        return RegExp(r'^[\w@%+=:,./-]+$').hasMatch(s) ? s : "'${s.replaceAll("'", r"'\''")}'";
      })
      .join(' ');
}

/// In a compose file a literal `$` is written `$$` (a lone `$` starts a
/// variable), and the server hands out every `$` of a variable's value
/// that way. The form shows the value itself and writes an edited one
/// escaped again; an untouched row goes back exactly as it came.
String unescapeEnvValue(String v) => v.replaceAll(r'$$', r'$');

String escapeEnvValue(String v) => v.replaceAll(r'$', r'$$');

/// Environment variable names that hold a secret, whose values the form
/// hides until revealed.
bool isSecretName(String key) => RegExp(r'PASS|PWD|SECRET|TOKEN|KEY|PRIVATE|CREDENTIAL|AUTH', caseSensitive: false).hasMatch(key);

/// One row of a list the form edits (a port, a folder, a variable, a
/// device). [raw] is the entry it was read from, null for a new row; a
/// row whose values didn't change is written back as [raw], untouched.
abstract class EditRow {
  EditRow(this.raw);

  final Object? raw;
  late final String _original = signature;

  /// The row's values in one string, for "did it change".
  String get signature;

  /// What kind of row, in the ids of its fields ([fieldId]).
  String get kind;

  bool get changed => raw == null || signature != _original;

  /// Call once the fields are set from [raw].
  void seal() => _original;
}

class PortRow extends EditRow {
  PortRow({this.hostIp = '', this.published = '', this.target = '', this.protocol = 'tcp', Object? raw}) : super(raw);

  factory PortRow.parse(Object? p) {
    if (p is Map) {
      return PortRow(
        hostIp: '${p['host_ip'] ?? ''}',
        published: p['published'] == null ? '' : '${p['published']}',
        target: p['target'] == null ? '' : '${p['target']}',
        protocol: '${p['protocol'] ?? 'tcp'}'.toLowerCase(),
        raw: p,
      )..seal();
    }
    var s = '$p';
    var protocol = 'tcp';
    final slash = s.lastIndexOf('/');
    if (slash >= 0) {
      protocol = s.substring(slash + 1).toLowerCase();
      s = s.substring(0, slash);
    }
    var hostIp = '';
    final v6 = RegExp(r'^\[([^\]]+)\]:(.*)$').firstMatch(s);
    if (v6 != null) {
      hostIp = v6.group(1)!;
      s = v6.group(2)!;
    }
    final parts = s.split(':');
    final target = parts.removeLast();
    final published = parts.isEmpty ? '' : parts.removeLast();
    if (parts.isNotEmpty) hostIp = parts.join(':');
    return PortRow(hostIp: hostIp, published: published, target: target, protocol: protocol, raw: p)..seal();
  }

  String hostIp;
  String published;
  String target;
  String protocol;

  @override
  String get signature => '$hostIp|$published|$target|$protocol';

  @override
  String get kind => 'port';

  Object? write({required bool asMap}) {
    if (!changed) return raw;
    final r = raw;
    if (r is Map || (r == null && asMap)) {
      final m = r is Map ? deepCopy(r) as Map<String, Object?> : <String, Object?>{};
      if (r == null) m['mode'] = 'ingress';
      m['target'] = int.tryParse(target.trim()) ?? target.trim();
      if (published.trim().isEmpty) {
        m.remove('published');
      } else {
        m['published'] = published.trim();
      }
      m['protocol'] = protocol;
      if (hostIp.trim().isEmpty) {
        m.remove('host_ip');
      } else {
        m['host_ip'] = hostIp.trim();
      }
      return m;
    }
    final ip = hostIp.trim().isEmpty ? '' : (hostIp.contains(':') ? '[${hostIp.trim()}]:' : '${hostIp.trim()}:');
    final proto = protocol == 'tcp' ? '' : '/$protocol';
    return published.trim().isEmpty ? '${target.trim()}$proto' : '$ip${published.trim()}:${target.trim()}$proto';
  }
}

class VolumeRow extends EditRow {
  VolumeRow({this.source = '', this.target = '', this.readOnly = false, this.type = 'bind', this.options = const [], Object? raw}) : super(raw);

  factory VolumeRow.parse(Object? v) {
    if (v is Map) {
      return VolumeRow(source: '${v['source'] ?? ''}', target: '${v['target'] ?? ''}', readOnly: v['read_only'] == true, type: '${v['type'] ?? 'bind'}', raw: v)
        ..seal();
    }
    final parts = '$v'.split(':');
    final opts = parts.length > 2 ? parts.sublist(2).join(':').split(',') : <String>[];
    final source = parts.length > 1 ? parts[0] : '';
    return VolumeRow(
      source: source,
      target: parts.length > 1 ? parts[1] : parts[0],
      readOnly: opts.contains('ro'),
      type: source.startsWith('/') || source.startsWith('.') || source.startsWith('~') || source.isEmpty ? 'bind' : 'volume',
      options: [
        for (final o in opts)
          if (o != 'ro' && o != 'rw') o,
      ],
      raw: v,
    )..seal();
  }

  String source;
  String target;
  bool readOnly;

  /// bind (a folder on the server), volume (a Docker volume: named, or
  /// anonymous with no source) or tmpfs (memory, no source).
  final String type;

  /// What's wrong with [source] as typed, or null. A row read from the
  /// file that wasn't touched is taken as it is.
  String? sourceProblem() => changed ? validateVolumeSource(source, type: type, isNew: raw == null) : null;
  final List<String> options;

  @override
  String get signature => '$source|$target|$readOnly';

  @override
  String get kind => 'vol';

  Object? write({required bool asMap}) {
    if (!changed) return raw;
    final r = raw;
    if (r is Map || (r == null && asMap)) {
      final m = r is Map ? deepCopy(r) as Map<String, Object?> : <String, Object?>{'type': type};
      m['source'] = source.trim();
      m['target'] = target.trim();
      if (readOnly) {
        m['read_only'] = true;
      } else {
        m.remove('read_only');
      }
      return m;
    }
    final opts = [if (readOnly) 'ro', ...options];
    return '${source.trim()}:${target.trim()}${opts.isEmpty ? '' : ':${opts.join(',')}'}';
  }
}

class EnvRow extends EditRow {
  EnvRow({this.key = '', this.value = '', this.wasNull = false, Object? raw}) : super(raw);

  String key;
  String value;

  /// `KEY:` with no value in the file (compose then takes it from the
  /// environment); kept while the row is untouched.
  final bool wasNull;

  @override
  String get signature => '$key=$value';

  @override
  String get kind => 'env';
}

class DeviceRow extends EditRow {
  DeviceRow({this.host = '', this.container = '', Object? raw}) : super(raw);

  factory DeviceRow.parse(Object? d) {
    if (d is Map) return DeviceRow(host: '${d['source'] ?? ''}', container: '${d['target'] ?? d['source'] ?? ''}', raw: d)..seal();
    final parts = '$d'.split(':');
    return DeviceRow(host: parts[0], container: parts.length > 1 ? parts[1] : parts[0], raw: d)..seal();
  }

  String host;
  String container;

  @override
  String get signature => '$host|$container';

  @override
  String get kind => 'dev';

  Object? write() {
    if (!changed) return raw;
    final r = raw;
    final c = container.trim().isEmpty ? host.trim() : container.trim();
    if (r is Map) {
      return (deepCopy(r) as Map<String, Object?>)
        ..['source'] = host.trim()
        ..['target'] = c;
    }
    final perms = r is String && r.split(':').length > 2 ? ':${r.split(':')[2]}' : '';
    return '${host.trim()}:$c$perms';
  }
}

/// The restart policies compose knows ('' is "not set": Docker's "no").
const restartPolicies = ['unless-stopped', 'always', 'on-failure', 'no'];

/// One service of the app, as the form edits it.
class ServiceForm {
  ServiceForm._(this.name);

  final String name;
  String repo = '';
  String tag = '';
  String? digest;

  /// False for a service built from source (`build:` and no `image:`),
  /// which the form doesn't ask an image of.
  bool hadImage = true;
  String restart = '';
  String networkMode = '';
  bool privileged = false;
  String caps = '';
  String cpus = '';
  String memoryMb = '';
  String command = '';
  List<PortRow> ports = [];
  List<VolumeRow> volumes = [];
  List<EnvRow> envs = [];
  List<DeviceRow> devices = [];

  /// Whether the file writes each list in long form (maps), as the server
  /// does, so new rows are written the same way.
  bool _portsAsMaps = true;
  bool _volumesAsMaps = true;
  bool _envAsMap = true;

  /// What the store says each variable is for ("TZ" → "Timezone").
  Map<String, String> envHelp = const {};

  String get image => joinImage(repo, tag, digest);

  factory ServiceForm.fromDoc(String name, Map s) {
    final f = ServiceForm._(name);
    final img = splitImage('${s['image'] ?? ''}');
    f.repo = img.repo;
    f.tag = img.tag;
    f.digest = img.digest;
    f.hadImage = s['image'] != null;
    f.restart = '${s['restart'] ?? ''}';
    f.networkMode = '${s['network_mode'] ?? ''}';
    f.privileged = s['privileged'] == true;
    final caps = s['cap_add'];
    f.caps = caps is List ? caps.join(', ') : '';
    final limits = _limits(s);
    f.cpus = limits?['cpus'] == null ? '' : '${limits!['cpus']}';
    f.memoryMb = memoryToMb(limits?['memory']);
    f.command = commandToText(s['command']);
    final ports = s['ports'];
    if (ports is List) {
      f.ports = [for (final p in ports) PortRow.parse(p)];
      if (ports.isNotEmpty) f._portsAsMaps = ports.any((p) => p is Map);
    }
    final vols = s['volumes'];
    if (vols is List) {
      f.volumes = [for (final v in vols) VolumeRow.parse(v)];
      if (vols.isNotEmpty) f._volumesAsMaps = vols.any((v) => v is Map);
    }
    final env = s['environment'];
    if (env is Map) {
      f.envs = [for (final e in env.entries) EnvRow(key: '${e.key}', value: e.value == null ? '' : unescapeEnvValue('${e.value}'), wasNull: e.value == null, raw: e)..seal()];
    } else if (env is List) {
      f._envAsMap = false;
      f.envs = [
        for (final e in env)
          () {
            final s = '$e';
            final eq = s.indexOf('=');
            return EnvRow(key: eq < 0 ? s : s.substring(0, eq), value: eq < 0 ? '' : unescapeEnvValue(s.substring(eq + 1)), wasNull: eq < 0, raw: e)..seal();
          }(),
      ];
    }
    final devices = s['devices'];
    if (devices is List) f.devices = [for (final d in devices) DeviceRow.parse(d)];
    final x = s['x-casaos'];
    final envMeta = x is Map ? x['envs'] : null;
    if (envMeta is List) {
      f.envHelp = {
        for (final m in envMeta.whereType<Map>())
          if (m['container'] != null && pickLocale(m['description']) != null) '${m['container']}': pickLocale(m['description'])!,
      };
    }
    return f;
  }

  static Map? _limits(Map s) {
    final deploy = s['deploy'];
    final res = deploy is Map ? deploy['resources'] : null;
    final limits = res is Map ? res['limits'] : null;
    return limits is Map ? limits : null;
  }

  /// The host ports this service publishes, for the Web UI port picker.
  List<String> get publishedTcpPorts => [
    for (final p in ports)
      if (p.protocol == 'tcp' && p.published.trim().isNotEmpty) p.published.trim(),
  ];

  static String _rowsSig(List<EditRow> rows) => rows.map((r) => '${r.raw == null ? '+' : ''}${r.signature}').join('\n');

  void _writeTo(Map<String, Object?> s, ServiceForm o) {
    if (repo != o.repo || tag != o.tag) {
      // A digest pins the old image: a new name or tag replaces it.
      s['image'] = joinImage(repo, tag);
    }
    if (restart != o.restart) _set(s, 'restart', restart.isEmpty ? null : restart);
    if (networkMode != o.networkMode) {
      if (networkMode.isEmpty) {
        s.remove('network_mode');
      } else {
        s['network_mode'] = networkMode;
        // Compose refuses network_mode next to networks.
        s.remove('networks');
      }
    }
    if (privileged != o.privileged) _set(s, 'privileged', privileged ? true : null);
    if (caps != o.caps) {
      final list = splitCaps(caps);
      _set(s, 'cap_add', list.isEmpty ? null : list);
    }
    if (cpus != o.cpus) _setLimit(s, 'cpus', cpus.trim().isEmpty || double.tryParse(cpus.trim()) == 0 ? null : cpus.trim());
    if (memoryMb != o.memoryMb) {
      final m = memoryMb.trim();
      _setLimit(s, 'memory', m.isEmpty || m == '0' ? null : (RegExp(r'^\d+$').hasMatch(m) ? '${m}M' : m));
    }
    if (command != o.command) _set(s, 'command', command.trim().isEmpty ? null : command.trim());
    if (_rowsSig(ports) != _rowsSig(o.ports)) {
      final list = [
        for (final p in ports)
          if (p.target.trim().isNotEmpty) p.write(asMap: _portsAsMaps),
      ];
      _set(s, 'ports', list.isEmpty ? null : list);
    }
    if (_rowsSig(volumes) != _rowsSig(o.volumes)) {
      final list = [
        for (final v in volumes)
          if (v.target.trim().isNotEmpty) v.write(asMap: _volumesAsMaps),
      ];
      _set(s, 'volumes', list.isEmpty ? null : list);
    }
    if (_rowsSig(envs) != _rowsSig(o.envs)) {
      final rows = envs.where((e) => e.key.trim().isNotEmpty);
      if (rows.isEmpty) {
        s.remove('environment');
      } else if (_envAsMap) {
        s['environment'] = <String, Object?>{for (final e in rows) e.key.trim(): e.changed ? escapeEnvValue(e.value) : (e.raw as MapEntry).value};
      } else {
        s['environment'] = [for (final e in rows) e.changed ? '${e.key.trim()}=${escapeEnvValue(e.value)}' : e.raw];
      }
    }
    if (_rowsSig(devices) != _rowsSig(o.devices)) {
      final list = [
        for (final d in devices)
          if (d.host.trim().isNotEmpty) d.write(),
      ];
      _set(s, 'devices', list.isEmpty ? null : list);
    }
  }

  static void _set(Map<String, Object?> m, String key, Object? v) {
    if (v == null) {
      m.remove(key);
    } else {
      m[key] = v;
    }
  }

  static void _setLimit(Map<String, Object?> s, String key, Object? v) {
    final deploy = s['deploy'] is Map ? s['deploy'] as Map<String, Object?> : <String, Object?>{};
    final res = deploy['resources'] is Map ? deploy['resources'] as Map<String, Object?> : <String, Object?>{};
    final limits = res['limits'] is Map ? res['limits'] as Map<String, Object?> : <String, Object?>{};
    _set(limits, key, v);
    // Don't leave empty deploy/resources/limits behind.
    _set(res, 'limits', limits.isEmpty ? null : limits);
    _set(deploy, 'resources', res.isEmpty ? null : res);
    _set(s, 'deploy', deploy.isEmpty ? null : deploy);
  }
}

List<String> splitCaps(String text) => [
  for (final c in text.split(RegExp(r'[\s,]+')))
    if (c.trim().isNotEmpty) c.trim().toUpperCase(),
];

/// The id of one field of the form: [part] of the app ("title", "port"),
/// of a [service] ("image", "tag"), or of one of its rows. The screen keys
/// its fields with it, so a problem can point at the field to fix.
String fieldId(String part, {ServiceForm? service, EditRow? row}) {
  if (service == null) return part;
  if (row == null) return '${service.name}/$part';
  return '${service.name}/${row.kind}/${identityHashCode(row)}/$part';
}

/// Something to fix before the form can be saved, and the field it is in.
class FormIssue {
  const FormIssue(this.message, this.field);

  final String message;

  /// The [fieldId] of the field to fix.
  final String field;

  @override
  String toString() => message;
}

/// A compose app's settings as the Edit app form shows them.
class ComposeForm {
  ComposeForm._(this.appName, this.mainService);

  /// The compose project name. The server only takes a file with the
  /// same `name:` back.
  final String appName;
  final String mainService;
  String title = '';
  String icon = '';
  String scheme = 'http';
  String hostname = '';
  String port = '';
  String index = '';

  /// The main service first.
  List<ServiceForm> services = [];

  static String mainServiceOf(Map doc) {
    final services = doc['services'] is Map ? doc['services'] as Map : const {};
    final x = doc['x-casaos'];
    final main = x is Map ? x['main']?.toString() : null;
    return main != null && services.containsKey(main) ? main : (services.isEmpty ? 'app' : '${services.keys.first}');
  }

  factory ComposeForm.fromDoc(Map doc) {
    final main = mainServiceOf(doc);
    final f = ComposeForm._('${doc['name'] ?? main}', main);
    final x = doc['x-casaos'] is Map ? doc['x-casaos'] as Map : const {};
    f.title = pickLocale(x['title']) ?? f.appName;
    f.icon = '${x['icon'] ?? ''}';
    f.scheme = '${x['scheme'] ?? ''}'.isEmpty ? 'http' : '${x['scheme']}';
    f.hostname = '${x['hostname'] ?? ''}';
    f.port = '${x['port_map'] ?? ''}';
    f.index = '${x['index'] ?? ''}';
    final services = doc['services'] is Map ? doc['services'] as Map : const {};
    f.services = [
      if (services[main] is Map) ServiceForm.fromDoc(main, services[main] as Map),
      for (final e in services.entries)
        if (e.key != main && e.value is Map) ServiceForm.fromDoc('${e.key}', e.value as Map),
    ];
    return f;
  }

  ServiceForm? service(String name) {
    for (final s in services) {
      if (s.name == name) return s;
    }
    return null;
  }

  /// Every TCP port the app publishes, main service first, for the Web UI
  /// port picker.
  List<String> get publishedPorts => {for (final s in services) ...s.publishedTcpPorts}.toList();

  /// The Web UI link, with [serverHost] standing in for a blank host.
  /// Null when there is no port or host to open.
  Uri? link(String serverHost) {
    final host = hostname.trim().isEmpty ? serverHost : hostname.trim();
    if (host.isEmpty || (port.trim().isEmpty && hostname.trim().isEmpty)) return null;
    var path = index.trim().isEmpty ? '/' : index.trim();
    if (!path.startsWith('/')) path = '/$path';
    return Uri.tryParse('$scheme://$host${port.trim().isEmpty ? '' : ':${port.trim()}'}$path');
  }

  /// A copy of [base] with this form's changes, compared field by field
  /// with the form [base] gives. Nothing else in [base] changes.
  Map<String, Object?> applyTo(Map base) {
    final doc = deepCopy(base) as Map<String, Object?>;
    final o = ComposeForm.fromDoc(base);
    final x = doc['x-casaos'] is Map ? doc['x-casaos'] as Map<String, Object?> : <String, Object?>{};
    var xChanged = false;
    void setX(String key, Object? v) {
      xChanged = true;
      if (v == null) {
        x.remove(key);
      } else {
        x[key] = v;
      }
    }

    if (title != o.title) {
      // Like the web form: the rename goes in `custom` (which the apps
      // list shows first) and in en_us.
      final old = x['title'];
      final t = old is Map ? Map<String, Object?>.from(old) : <String, Object?>{};
      t['en_us'] = title.trim();
      if (t.containsKey('custom') || old is! Map) t['custom'] = title.trim();
      setX('title', t);
    }
    if (icon != o.icon) setX('icon', icon.trim().isEmpty ? null : icon.trim());
    if (scheme != o.scheme) setX('scheme', scheme);
    if (hostname != o.hostname) setX('hostname', hostname.trim());
    if (port != o.port) setX('port_map', port.trim());
    if (index != o.index) setX('index', index.trim());
    if (xChanged) doc['x-casaos'] = x;

    final services = doc['services'] as Map<String, Object?>;
    for (final s in services.keys.toList()) {
      final form = service(s);
      final orig = o.service(s);
      final raw = services[s];
      if (form == null || orig == null || raw is! Map) continue;
      final copy = raw as Map<String, Object?>;
      form._writeTo(copy, orig);
    }
    return doc;
  }

  /// Everything wrong with the form, as sentences, for the summary above
  /// the form and to block Save. The fields show the same messages inline.
  List<String> problems() => [for (final i in issues()) i.message];

  /// [problems], each with the field it is in, in the form's order.
  List<FormIssue> issues() {
    final out = <FormIssue>[];
    void add(String? e, String? where, String field) {
      if (e != null) out.add(FormIssue(where == null ? e : '$where: $e', field));
    }

    add(validateTitle(title), 'Name', 'title');
    add(validateIconUrl(icon), 'Icon', 'icon');
    add(validateHost(hostname), 'Web UI host', 'hostname');
    add(validateWebUiPort(port), 'Web UI port', 'port');
    add(validatePath(index), 'Web UI path', 'index');
    final seen = <String>{};
    for (final s in services) {
      final where = services.length > 1 ? s.name : null;
      String label(String l) => where == null ? l : '$where, ${l.toLowerCase()}';
      String id(String part, [EditRow? row]) => fieldId(part, service: s, row: row);
      if (s.hadImage || s.repo.trim().isNotEmpty || s.tag.trim().isNotEmpty) add(validateImageRepo(s.repo), label('Image'), id('repo'));
      add(validateTag(s.tag), label('Tag'), id('tag'));
      // Rows read from the file and left alone are taken as they are: the
      // server runs them today, even in shapes the form doesn't check
      // for (a tmpfs, a CDI device). Only what the user typed is checked.
      for (final p in s.ports) {
        if (p.target.trim().isEmpty && p.published.trim().isEmpty) continue;
        if (p.changed) {
          add(validatePort(p.published, required: false), label('Server port'), id('published', p));
          add(validatePort(p.target, required: true), label('Container port'), id('target', p));
        }
        final k = '${p.hostIp.trim()}|${p.published.trim()}/${p.protocol}';
        if (p.published.trim().isNotEmpty && !seen.add(k)) {
          out.add(FormIssue('Server port ${p.published.trim()}/${p.protocol.toUpperCase()} is used twice.', id('published', p)));
        }
      }
      for (final v in s.volumes) {
        if (!v.changed || (v.source.trim().isEmpty && v.target.trim().isEmpty)) continue;
        add(v.sourceProblem(), label('Folder on the server'), id('source', v));
        add(validateContainerPath(v.target), label('Path in the app'), id('target', v));
      }
      final keys = <String>{};
      for (final e in s.envs) {
        if (e.key.trim().isEmpty && e.value.isEmpty) continue;
        if (e.changed) add(validateEnvKey(e.key), label('Variable'), id('key', e));
        if (e.key.trim().isNotEmpty && !keys.add(e.key.trim())) out.add(FormIssue('${where == null ? '' : '$where: '}${e.key.trim()} is set twice.', id('key', e)));
      }
      for (final d in s.devices) {
        if (!d.changed || (d.host.trim().isEmpty && d.container.trim().isEmpty)) continue;
        add(validateDevice(d.host), label('Device'), id('host', d));
      }
      add(validateCpus(s.cpus), label('CPU limit'), id('cpus'));
      add(validateMemory(s.memoryMb), label('Memory limit'), id('memory'));
      add(validateCaps(s.caps), label('Capabilities'), id('caps'));
    }
    return out;
  }
}

// ---------------------------------------------------------------------------
// Validation: null when fine, else what to fix (a short sentence).
// ---------------------------------------------------------------------------

String? validateTitle(String v) => v.trim().isEmpty ? 'Give the app a name' : null;

String? validateIconUrl(String v) {
  final s = v.trim();
  if (s.isEmpty) return null;
  final u = Uri.tryParse(s);
  if (u == null || !(u.scheme == 'http' || u.scheme == 'https') || u.host.isEmpty) return 'Use a web address that starts with https://';
  return null;
}

/// The Web UI host: a name or IPv4 address, or an IPv6 address in
/// brackets. The port has its own field, so `nas:8080` is refused (it would
/// make `http://nas:8080:8097/`).
String? validateHost(String v) {
  final s = v.trim();
  if (s.isEmpty) return null;
  if (s.contains('://')) return 'Only the host name, without http://';
  if (RegExp(r'^[A-Za-z0-9.\-]+$').hasMatch(s) || RegExp(r'^\[[0-9A-Fa-f:.]+\]$').hasMatch(s)) return null;
  if (RegExp(r'^[A-Za-z0-9.\-]+:\d*$').hasMatch(s)) return 'Only the host name: put the port in Port below';
  if (!s.startsWith('[') && s.contains(':') && RegExp(r'^[0-9A-Fa-f:.]+$').hasMatch(s)) return 'Put an IPv6 address in brackets, like [fd00::1]';
  return 'Use a host name like media.example.com or an IP address';
}

final _portRe = RegExp(r'^\d+(-\d+)?$');

String? validatePort(String v, {required bool required}) {
  final s = v.trim();
  if (s.isEmpty) return required ? 'Enter a port' : null;
  if (!_portRe.hasMatch(s) || s.split('-').any((n) => int.parse(n) < 1 || int.parse(n) > 65535)) return 'Use a port from 1 to 65535';
  return null;
}

/// The Web UI port: one port, not a range - it goes into the link.
String? validateWebUiPort(String v) {
  final s = v.trim();
  if (s.isEmpty) return null;
  final n = int.tryParse(s);
  if (!RegExp(r'^\d+$').hasMatch(s) || n == null || n < 1 || n > 65535) return 'Use a port from 1 to 65535';
  return null;
}

String? validatePath(String v) {
  final s = v.trim();
  if (s.isEmpty) return null;
  if (!s.startsWith('/')) return 'Start the path with /';
  if (s.contains(' ')) return 'No spaces in the path';
  return null;
}

String? validateImageRepo(String v) {
  final s = v.trim();
  if (s.isEmpty) return 'Enter the image, like linuxserver/jellyfin';
  if (RegExp(r'\s').hasMatch(s) || s.contains('@')) return 'No spaces or @ in the image name';
  if (s != s.toLowerCase() && !s.contains('/')) return 'Image names are lower case';
  return null;
}

String? validateTag(String v) {
  final s = v.trim();
  if (s.isEmpty) return null;
  if (!RegExp(r'^[\w][\w.-]{0,127}$').hasMatch(s)) return 'Letters, numbers, . - and _ only';
  return null;
}

/// The source of a folder row of [type] (bind, volume or tmpfs). A tmpfs
/// has none, and a volume without one is anonymous - only a new volume
/// row, or a bind, needs it.
String? validateVolumeSource(String v, {String type = 'bind', bool isNew = true}) {
  final s = v.trim();
  if (type == 'tmpfs') return null;
  if (type == 'volume') return s.isEmpty && isNew ? 'Enter the volume name' : null;
  if (s.isEmpty) return 'Enter a folder on the server';
  if (!s.startsWith('/')) return 'Use a full path, starting with /';
  return null;
}

String? validateContainerPath(String v) {
  final s = v.trim();
  if (s.isEmpty) return 'Enter the path inside the app';
  if (!s.startsWith('/')) return 'Start the path with /';
  return null;
}

String? validateEnvKey(String v) {
  final s = v.trim();
  if (s.isEmpty) return 'Enter a name';
  if (!RegExp(r'^[A-Za-z_][A-Za-z0-9_.-]*$').hasMatch(s)) return 'Letters, numbers and _ only, not starting with a number';
  return null;
}

String? validateDevice(String v) {
  final s = v.trim();
  if (s.isEmpty) return 'Enter a device, like /dev/dri';
  // A CDI name (vendor.com/class=name, like nvidia.com/gpu=all) is fine.
  if (!s.startsWith('/') && !s.contains('=')) return 'Use a full path, like /dev/dri';
  return null;
}

String? validateCpus(String v) {
  final s = v.trim();
  if (s.isEmpty) return null;
  final n = double.tryParse(s);
  if (n == null || n < 0) return 'Use a number of cores, like 1.5';
  return null;
}

String? validateMemory(String v) {
  final s = v.trim();
  if (s.isEmpty) return null;
  if (!RegExp(r'^\d+$').hasMatch(s)) return 'Use a whole number of megabytes';
  return null;
}

String? validateCaps(String v) {
  for (final c in splitCaps(v)) {
    if (!RegExp(r'^[A-Z_]+$').hasMatch(c)) return '“$c” is not a capability name, like NET_ADMIN';
  }
  return null;
}

/// What's wrong with a compose file typed in the YAML editor for the app
/// [appName], or null when the server should take it.
String? validateComposeText(String text, String appName) {
  try {
    final doc = parseComposeYaml(text);
    final name = doc['name']?.toString();
    if (name != appName) return 'Keep the first line as “name: $appName”: the server only saves the file of the same app.';
    return null;
  } on FormatException catch (e) {
    return e.message;
  }
}
