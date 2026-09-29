import 'package:flutter/foundation.dart';

import 'api_client.dart';

/// The two families of persistent terminal sessions
/// (docs/specs/2026-09-29-terminal-sessions.md): shells on the server
/// (core) and shells in app containers (app-management). Same JSON and
/// WebSocket protocol, different base paths.
enum TerminalFamily {
  host('/v1/sys/terminal-sessions', '/v1/sys/wsterm'),
  container('/v1/container/terminal-sessions', null);

  const TerminalFamily(this.base, this.legacyPath);

  /// `GET` lists, `POST` creates, `{base}/:id` gets, renames, ends.
  final String base;

  /// The pre-sessions plain-connect route, for servers that don't have
  /// sessions yet. Containers build theirs from the container id.
  final String? legacyPath;

  static TerminalFamily parse(Object? kind) => kind == 'container' ? container : host;
}

/// Why a session ended (`exit_reason`).
enum TerminalExitReason { exited, killed, timeout, evicted, unknown }

/// One terminal session on the server.
@immutable
class TerminalSession {
  const TerminalSession({
    required this.id,
    required this.title,
    required this.family,
    required this.running,
    required this.attachPath,
    this.user,
    this.container,
    this.containerId,
    this.shell,
    this.exitCode,
    this.exitReason = TerminalExitReason.unknown,
    this.exitedAt,
    this.createdAt,
    this.lastActivityAt,
    this.clients = 0,
    this.cols,
    this.rows,
    this.cwd,
    this.command,
    this.scrollbackBytes = 0,
    this.legacy = false,
  });

  factory TerminalSession.fromJson(Map<String, dynamic> j) {
    final id = j['id']?.toString() ?? '';
    final family = TerminalFamily.parse(j['kind']);
    String? str(String k) {
      final v = j[k]?.toString().trim();
      return v == null || v.isEmpty ? null : v;
    }

    int? integer(String k) => j[k] is num ? (j[k] as num).toInt() : int.tryParse('${j[k] ?? ''}');
    DateTime? time(String k) {
      final v = str(k);
      return v == null ? null : DateTime.tryParse(v);
    }

    return TerminalSession(
      id: id,
      title: str('title') ?? 'Terminal',
      family: family,
      running: j['state'] != 'exited',
      attachPath: str('attach_path') ?? '${family.base}/$id/attach',
      user: str('user'),
      container: str('container'),
      containerId: str('container_id'),
      shell: str('shell'),
      exitCode: integer('exit_code'),
      exitReason: switch (j['exit_reason']) {
        'exited' => TerminalExitReason.exited,
        'killed' => TerminalExitReason.killed,
        'timeout' => TerminalExitReason.timeout,
        'evicted' => TerminalExitReason.evicted,
        _ => TerminalExitReason.unknown,
      },
      exitedAt: time('exited_at'),
      createdAt: time('created_at'),
      lastActivityAt: time('last_activity_at'),
      clients: integer('clients') ?? 0,
      cols: integer('cols'),
      rows: integer('rows'),
      cwd: str('cwd'),
      command: str('command'),
      scrollbackBytes: integer('scrollback_bytes') ?? 0,
      legacy: j['legacy'] == true,
    );
  }

  final String id;
  final String title;
  final TerminalFamily family;

  /// False once the shell has ended (`state: exited`); the server keeps it
  /// listed for a while so its last output can be read.
  final bool running;
  final String attachPath;

  /// The host account (host sessions only).
  final String? user;

  /// The container's name (container sessions only).
  final String? container;
  final String? containerId;
  final String? shell;
  final int? exitCode;
  final TerminalExitReason exitReason;
  final DateTime? exitedAt;
  final DateTime? createdAt;
  final DateTime? lastActivityAt;

  /// Viewers attached right now (this phone included, when it is).
  final int clients;
  final int? cols;
  final int? rows;

  /// The foreground job's directory and program, e.g. `vim` in
  /// `/home/alex/projects`. Best effort.
  final String? cwd;
  final String? command;
  final int scrollbackBytes;

  /// Opened by an old app through the plain-connect route.
  final bool legacy;

  bool get isContainer => family == TerminalFamily.container;

  /// The shell's own name ("bash"), without its path.
  String? get shellName {
    final s = shell;
    if (s == null) return null;
    final i = s.lastIndexOf('/');
    return i < 0 ? s : s.substring(i + 1);
  }

  /// The program in the foreground, when it isn't just the shell: "vim",
  /// "htop", "docker compose logs".
  String? get foregroundJob {
    final c = command;
    if (c == null) return null;
    final name = c.split(' ').first;
    final bare = name.contains('/') ? name.substring(name.lastIndexOf('/') + 1) : name;
    final shells = {'bash', 'sh', 'zsh', 'fish', 'ash', 'dash', '-bash', '-sh', '-zsh', 'login', shellName};
    return shells.contains(bare) ? null : c;
  }

  /// [cwd] with the home directory shortened to `~`.
  String? get shortCwd {
    final d = cwd;
    if (d == null) return null;
    final u = user;
    if (u != null) {
      final home = u == 'root' ? '/root' : '/home/$u';
      if (d == home) return '~';
      if (d.startsWith('$home/')) return '~${d.substring(home.length)}';
    }
    return d;
  }

  /// What it is doing, for a list row: "vim · ~/projects", "~/projects",
  /// or the container.
  String get activity {
    final parts = [?foregroundJob, ?shortCwd];
    return parts.join(' · ');
  }

  /// How it ended, in words.
  String get endedText => switch (exitReason) {
        TerminalExitReason.killed => 'Ended',
        TerminalExitReason.timeout => 'Closed after a day unused',
        TerminalExitReason.evicted => 'Closed to make room for a new one',
        _ => exitCode == null || exitCode! < 0 ? 'The shell ended' : 'The shell ended (exit code $exitCode)',
      };

  TerminalSession copyWith({String? title, int? clients}) => TerminalSession(
        id: id,
        title: title ?? this.title,
        family: family,
        running: running,
        attachPath: attachPath,
        user: user,
        container: container,
        containerId: containerId,
        shell: shell,
        exitCode: exitCode,
        exitReason: exitReason,
        exitedAt: exitedAt,
        createdAt: createdAt,
        lastActivityAt: lastActivityAt,
        clients: clients ?? this.clients,
        cols: cols,
        rows: rows,
        cwd: cwd,
        command: command,
        scrollbackBytes: scrollbackBytes,
        legacy: legacy,
      );

  @override
  bool operator ==(Object other) => other is TerminalSession && other.id == id && other.family == family;

  @override
  int get hashCode => Object.hash(id, family);
}

/// The server has no persistent sessions for [family] (an older NivaroOS:
/// the route answers 404). Terminals then use the plain-connect route and
/// end when closed.
class TerminalSessionsUnsupported implements Exception {
  const TerminalSessionsUnsupported(this.family);
  final TerminalFamily family;

  @override
  String toString() => 'This server keeps no terminal sessions. Update NivaroOS to keep terminals running.';
}

/// Both families' sessions, merged.
@immutable
class TerminalSessionList {
  const TerminalSessionList({
    this.sessions = const [],
    this.unsupported = const {},
    this.errors = const {},
    this.maxSessions,
    this.detachedTimeout,
  });

  /// Newest first.
  final List<TerminalSession> sessions;

  /// Families the server has no sessions for (an older server).
  final Set<TerminalFamily> unsupported;

  /// Families whose list couldn't be loaded, with the reason.
  final Map<TerminalFamily, String> errors;

  /// Running sessions allowed per family.
  final int? maxSessions;

  /// How long a detached, idle session is kept; null when never.
  final Duration? detachedTimeout;

  List<TerminalSession> get running => [for (final s in sessions) if (s.running) s];
  List<TerminalSession> get ended => [for (final s in sessions) if (!s.running) s];

  /// Nothing could be loaded at all.
  bool get failed => errors.length + unsupported.length == TerminalFamily.values.length && errors.isNotEmpty;
}

/// Talks to the terminal-session routes. One instance; tests replace the
/// HTTP client through `package:http`'s zone like every other API.
class TerminalSessionsApi {
  TerminalSessionsApi._();
  static final TerminalSessionsApi instance = TerminalSessionsApi._();

  /// Running sessions of both families as last seen (null until known), for
  /// the count on the Terminal row.
  final ValueNotifier<int?> runningCount = ValueNotifier(null);

  // Families this server answered 404 for, per server address, so a
  // new terminal on an old server goes straight to the legacy route.
  final Map<String, Set<TerminalFamily>> _unsupported = {};

  Set<TerminalFamily> get _unsupportedHere => _unsupported.putIfAbsent(ApiClient.instance.baseUrl, () => {});

  /// Whether [family] is known to have no sessions on this server. Null
  /// until a list call has said.
  bool? supports(TerminalFamily family) => _unsupportedHere.contains(family) ? false : (_known.contains('${ApiClient.instance.baseUrl}|${family.name}') ? true : null);
  final Set<String> _known = {};

  /// One family's sessions. Throws [TerminalSessionsUnsupported] on an
  /// older server.
  Future<({List<TerminalSession> sessions, int? max, int? timeoutSeconds})> listFamily(TerminalFamily family, {String? container}) async {
    try {
      final res = await ApiClient.instance.get(family.base, query: {'container': ?container});
      _known.add('${ApiClient.instance.baseUrl}|${family.name}');
      _unsupportedHere.remove(family);
      final data = res['data'];
      final list = data is Map ? data['sessions'] : null;
      final limits = data is Map && data['limits'] is Map ? data['limits'] as Map : const {};
      return (
        sessions: [
          if (list is List)
            for (final e in list)
              if (e is Map<String, dynamic>) TerminalSession.fromJson(e),
        ],
        max: (limits['max_sessions'] as num?)?.toInt(),
        timeoutSeconds: (limits['detached_timeout_seconds'] as num?)?.toInt(),
      );
    } on ApiException catch (e) {
      if (e.statusCode == 404 || e.statusCode == 405) {
        _unsupportedHere.add(family);
        throw TerminalSessionsUnsupported(family);
      }
      rethrow;
    }
  }

  /// Both families, merged newest first. A family that fails is reported
  /// in [TerminalSessionList.errors]; the other still shows. With
  /// [container], only that container's shells.
  Future<TerminalSessionList> list({String? container}) async {
    final families = container == null ? TerminalFamily.values : const [TerminalFamily.container];
    final results = await Future.wait([
      for (final f in families)
        listFamily(f, container: f == TerminalFamily.container ? container : null).then<Object>((r) => r, onError: (Object e) => e),
    ]);
    final sessions = <TerminalSession>[];
    final unsupported = <TerminalFamily>{};
    final errors = <TerminalFamily, String>{};
    int? max;
    Duration? timeout;
    for (var i = 0; i < families.length; i++) {
      final r = results[i];
      if (r is TerminalSessionsUnsupported) {
        unsupported.add(families[i]);
      } else if (r is ({List<TerminalSession> sessions, int? max, int? timeoutSeconds})) {
        sessions.addAll(r.sessions);
        max ??= r.max;
        final t = r.timeoutSeconds;
        if (t != null && t > 0) timeout ??= Duration(seconds: t);
      } else {
        errors[families[i]] = r is ApiException ? r.message : "Couldn't load the terminals";
      }
    }
    sessions.sort((a, b) {
      // Running first, then the most recently used.
      if (a.running != b.running) return a.running ? -1 : 1;
      final ta = a.lastActivityAt ?? a.createdAt ?? DateTime(0);
      final tb = b.lastActivityAt ?? b.createdAt ?? DateTime(0);
      return tb.compareTo(ta);
    });
    final result = TerminalSessionList(sessions: sessions, unsupported: unsupported, errors: errors, maxSessions: max, detachedTimeout: timeout);
    if (container == null && errors.isEmpty) runningCount.value = result.running.length;
    return result;
  }

  /// Starts a session at [cols] x [rows]. Throws [TerminalSessionsUnsupported]
  /// on an older server, [ApiException] otherwise (409: the container
  /// isn't running, 429: too many sessions).
  Future<TerminalSession> create(TerminalFamily family, {required int cols, required int rows, String? container, String? shell, String? title}) async {
    if (_unsupportedHere.contains(family)) throw TerminalSessionsUnsupported(family);
    if (supports(family) == null) {
      // Find out first: on an old server the POST would 404 the same way a
      // missing container does.
      await listFamily(family);
    }
    final res = await ApiClient.instance.post(family.base, body: {
      'cols': cols,
      'rows': rows,
      'container': ?container,
      'shell': ?shell,
      'title': ?title,
    });
    final data = res['data'];
    if (data is! Map<String, dynamic>) throw ApiException("The server didn't start the terminal");
    final s = TerminalSession.fromJson(data);
    final n = runningCount.value;
    if (n != null) runningCount.value = n + 1;
    return s;
  }

  Future<TerminalSession> get(TerminalSession s) async {
    final res = await ApiClient.instance.get('${s.family.base}/${s.id}');
    final data = res['data'];
    if (data is! Map<String, dynamic>) throw ApiException('The terminal is gone', statusCode: 404);
    return TerminalSession.fromJson(data);
  }

  Future<TerminalSession> rename(TerminalSession s, String title) async {
    final res = await ApiClient.instance.put('${s.family.base}/${s.id}', body: {'title': title});
    final data = res['data'];
    return data is Map<String, dynamic> ? TerminalSession.fromJson(data) : s.copyWith(title: title);
  }

  /// Ends a running session, or dismisses an ended one.
  Future<void> end(TerminalSession s) async {
    try {
      await ApiClient.instance.delete('${s.family.base}/${s.id}');
    } on ApiException catch (e) {
      // Already gone is what we wanted.
      if (e.statusCode != 404) rethrow;
    }
    final n = runningCount.value;
    if (s.running && n != null && n > 0) runningCount.value = n - 1;
  }

  /// Refreshes [runningCount] quietly.
  Future<void> refreshCount() async {
    try {
      await list();
    } catch (_) {}
  }

  @visibleForTesting
  void debugReset() {
    _unsupported.clear();
    _known.clear();
    runningCount.value = null;
  }
}
