// The server's self-updater (GET /v1/sys/update/status; the server's
// docs/guides/server-updates.md): which server release is installed, what is
// on offer, what it is doing now and how the last update went. Parsing and
// the words live here so they can be unit tested
// (test/models/self_update_test.dart).

class SelfUpdate {
  const SelfUpdate({
    this.state = 'idle',
    this.step = '',
    this.progress = 0,
    this.current = '',
    this.available = '',
    this.availableSize = 0,
    this.notes = '',
    this.result,
    this.skipped = '',
    this.lastError = '',
    this.unsupported = '',
    this.running = false,
    this.canRollback = false,
    this.auto = 'install',
    this.window = '04:00',
  });

  final String state;
  final String step;
  final int progress;

  /// The installed release, '' for a server built from source.
  final String current;

  /// A newer release this server can install, else ''.
  final String available;
  final int availableSize;
  final String notes;
  final SelfUpdateResult? result;
  final String skipped;
  final String lastError;
  final String unsupported;
  final bool running;
  final bool canRollback;
  final String auto;
  final String window;

  static const _busy = {'waiting', 'downloading', 'verifying', 'applying', 'probation', 'rolling_back'};

  bool get busy => running || _busy.contains(state);

  factory SelfUpdate.fromJson(Map<dynamic, dynamic> d) {
    final st = d['state'] is Map ? d['state'] as Map : const {};
    final a = st['available'] is Map ? st['available'] as Map : const {};
    final s = d['settings'] is Map ? d['settings'] as Map : const {};
    final r = st['last_result'];
    return SelfUpdate(
      state: st['state']?.toString() ?? 'idle',
      step: st['step']?.toString() ?? '',
      progress: (st['progress'] as num?)?.toInt() ?? 0,
      current: st['current']?.toString() ?? '',
      available: a['version']?.toString() ?? '',
      availableSize: (a['size'] as num?)?.toInt() ?? 0,
      notes: a['notes_md']?.toString().trim() ?? '',
      result: r is Map ? SelfUpdateResult.fromJson(r) : null,
      skipped: st['skipped']?.toString() ?? '',
      lastError: st['last_error']?.toString() ?? '',
      unsupported: st['unsupported']?.toString() ?? '',
      running: d['running'] == true,
      canRollback: d['can_rollback'] == true,
      auto: s['auto']?.toString() ?? 'install',
      window: s['window']?.toString() ?? '04:00',
    );
  }

  /// The step in progress, in words ('' when idle).
  String get stepText => switch (state) {
        'waiting' => 'Waiting for running backups to finish',
        'downloading' => progress > 0 ? 'Downloading ($progress%)' : 'Downloading',
        'verifying' => 'Checking the signature',
        'probation' => 'Installed - watching it for 10 minutes',
        'rolling_back' => 'Putting the previous version back',
        'applying' => switch (step) {
            'stopping' => 'Stopping the services that changed',
            'migrating' => 'Updating settings',
            'health' => 'Checking that everything runs',
            _ => 'Installing',
          },
        _ => running ? 'Starting' : '',
      };

  /// How updates arrive, for the group's footer.
  String get scheduleText => auto == 'notify'
      ? 'New server releases are announced; you install them here or in Settings › Updates.'
      : 'New server releases install at about $window when no backup, transfer, VM console or Host Desktop session is running.';
}

class SelfUpdateResult {
  const SelfUpdateResult({required this.result, this.from = '', this.to = '', this.reason = ''});

  final String result;
  final String from;
  final String to;
  final String reason;

  factory SelfUpdateResult.fromJson(Map<dynamic, dynamic> r) => SelfUpdateResult(
        result: r['result']?.toString() ?? '',
        from: r['from']?.toString() ?? '',
        to: r['to']?.toString() ?? '',
        reason: r['reason']?.toString() ?? '',
      );

  bool get failed => result != 'installed';

  /// What was installed, or why it was rolled back.
  String get text {
    final f = from == 'source' ? 'the build from source' : from;
    return switch (result) {
      'installed' => 'Updated to $to',
      'rolled_back' => reason.isEmpty ? '$to was rolled back to $f' : '$to was rolled back to $f: $reason',
      'stuck' => '$to failed and the previous version is not healthy either',
      _ => '$to was not installed${reason.isEmpty ? '' : ': $reason'}',
    };
  }
}
