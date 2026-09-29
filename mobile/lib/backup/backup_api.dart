// The Backup & Sync REST API (docs/specs/backup-api.json), reached through
// the gateway at /v1/backup with the app's session. Every answer is the
// standard envelope; errors carry an ErrorBody {error_code, field_errors}
// (services/backup/jobs/apitypes.go), which [BackupError] turns into the
// web's words for that code. Writes need an administrator; a 403 says so.

import '../services/api_client.dart';
import 'backup_models.dart';
import 'backup_strings.dart';

/// A failed backup request, in the web's words.
class BackupError implements Exception {
  BackupError(this.code, {this.fieldErrors = const {}, this.cause, this.statusCode});

  /// The service's error_code ("forbidden", "invalid_state", "validation"),
  /// or '' when the server wasn't reached.
  final String code;

  /// A 400's field_errors: JSON field path -> field or error code.
  final Map<String, String> fieldErrors;

  /// The underlying failure (unreachable server, session ended).
  final ApiException? cause;
  final int? statusCode;

  bool get isUnreachable => cause?.isUnreachable ?? false;

  static String _known(String code) => btHas('backup.err.$code.title') ? code : 'internal';

  /// One line: the code's title, or why the server couldn't be reached.
  String get title => code.isEmpty ? (cause?.message ?? bt('backup.err.internal.title')) : bt('backup.err.${_known(code)}.title');
  String get causeText => code.isEmpty ? '' : bt('backup.err.${_known(code)}.cause');
  String get fix => code.isEmpty ? '' : bt('backup.err.${_known(code)}.fix');

  /// A field error as a sentence (backup.field.<code>, else the code's title).
  static String fieldText(String value) =>
      btHas('backup.field.$value') ? bt('backup.field.$value') : bt('backup.err.${_known(value)}.title');

  /// The first field error at [prefix] ("dest", "sources", "triggers").
  String? fieldError(String prefix) {
    for (final e in fieldErrors.entries) {
      if (e.key == prefix || e.key.startsWith('$prefix.') || e.key.startsWith('$prefix[')) return fieldText(e.value);
    }
    return null;
  }

  factory BackupError.from(Object e) {
    if (e is BackupError) return e;
    if (e is! ApiException) return BackupError('internal');
    final data = e.data;
    var code = '';
    final fields = <String, String>{};
    if (data is Map) {
      code = data['error_code']?.toString() ?? '';
      final fe = data['field_errors'];
      if (fe is Map) fe.forEach((k, v) => fields[k.toString()] = v.toString());
    }
    // The envelope's message is the error code too.
    if (code.isEmpty && e.statusCode != null && e.statusCode! >= 400 && RegExp(r'^[a-z_]+$').hasMatch(e.message)) code = e.message;
    if (code.isEmpty && e.statusCode == 403) code = 'forbidden';
    if (code.isEmpty && e.statusCode == 404) code = 'not_found';
    if (code.isEmpty && e.statusCode == 429) code = 'rate_limited';
    return BackupError(code, fieldErrors: fields, cause: e.isUnreachable || e.kind == ApiErrorKind.auth ? e : null, statusCode: e.statusCode);
  }

  @override
  String toString() => title;
}

/// Typed calls. One instance per screen is fine; it holds no state.
class BackupApi {
  BackupApi([ApiClient? client]) : _c = client ?? ApiClient.instance;

  final ApiClient _c;

  static const base = '/backup';

  Future<T> _call<T>(Future<Map<String, dynamic>> Function() request, T Function(Object? data) parse) async {
    try {
      final res = await request();
      return parse(res['data']);
    } catch (e) {
      throw BackupError.from(e);
    }
  }

  /// Whether the optional module is installed and answering. Any failure
  /// (no route, stopped, a web page instead of JSON) means it isn't.
  Future<BackupHealth> health() async {
    try {
      // Not enveloped: the whole answer is the health.
      final res = await _c.get('$base/health');
      return BackupHealth.fromJson(res);
    } catch (_) {
      return const BackupHealth(installed: false, running: false);
    }
  }

  Future<List<BackupJob>> jobs() => _call(() => _c.get('$base/jobs'), (d) => [for (final j in d is List ? d : const []) BackupJob.fromJson(j)]);

  Future<BackupJob> job(String id) => _call(() => _c.get('$base/jobs/${Uri.encodeComponent(id)}'), BackupJob.fromJson);

  Future<BackupJob> toggle(String id, bool enabled) =>
      _call(() => _c.post('$base/jobs/${Uri.encodeComponent(id)}/toggle', body: {'enabled': enabled}), BackupJob.fromJson);

  /// Starts a run (or joins the one already queued); its id.
  Future<String> run(String id, {bool preview = false}) =>
      _call(() => _c.post('$base/jobs/${Uri.encodeComponent(id)}/run', body: {'preview': preview}), (d) => (d as Map?)?['run_id']?.toString() ?? '');

  Future<BackupJob> create(Map<String, Object?> job) => _call(() => _c.post('$base/jobs', body: job), BackupJob.fromJson);

  /// Saves an edit; [job] carries the revision it was read at (409 when
  /// someone else saved in between).
  Future<BackupJob> update(String id, Map<String, Object?> job) => _call(() => _c.put('$base/jobs/${Uri.encodeComponent(id)}', body: job), BackupJob.fromJson);

  Future<CronPreview> cronPreview(String cron) => _call(() => _c.post('$base/cron/preview', body: {'cron': cron}), CronPreview.fromJson);

  Future<RunList> runs({String? jobId, int limit = 30, String? before}) => _call(
        () => _c.get('$base/runs', query: {'job_id': ?jobId, 'limit': '$limit', if (before != null && before.isNotEmpty) 'before': before}),
        RunList.fromJson,
      );

  Future<BackupRun> getRun(String id) => _call(() => _c.get('$base/runs/${Uri.encodeComponent(id)}'), BackupRun.fromJson);

  Future<LogPage> log(String runId, {int after = 0, int limit = 500}) =>
      _call(() => _c.get('$base/runs/${Uri.encodeComponent(runId)}/log', query: {'after': '$after', 'limit': '$limit'}), LogPage.fromJson);

  Future<BackupRun> cancel(String runId) => _call(() => _c.post('$base/runs/${Uri.encodeComponent(runId)}/cancel'), BackupRun.fromJson);

  /// Answers a waiting run: go on [mode] "as_shown" or "copy_once", or
  /// (proceed false) cancel it.
  Future<BackupRun> decide(String runId, {required bool proceed, String? mode}) => _call(
        () => _c.post('$base/runs/${Uri.encodeComponent(runId)}/decide', body: {'proceed': proceed, if (proceed) 'mode': mode ?? 'as_shown'}),
        BackupRun.fromJson,
      );

  Future<PreviewPage> preview(String runId, {required String op, String q = '', int offset = 0, int limit = 200}) => _call(
        () => _c.get('$base/runs/${Uri.encodeComponent(runId)}/preview', query: {'op': op, if (q.isNotEmpty) 'q': q, 'offset': '$offset', 'limit': '$limit'}),
        PreviewPage.fromJson,
      );

  Future<List<BackupVersion>> versions(String jobId) =>
      _call(() => _c.get('$base/jobs/${Uri.encodeComponent(jobId)}/versions'), (d) => [for (final v in d is List ? d : const []) BackupVersion.fromJson(v)]);

  Future<BrowseResult> browseVersion(String jobId, String versionId, String path) => _call(
        () => _c.get('$base/jobs/${Uri.encodeComponent(jobId)}/versions/${Uri.encodeComponent(versionId)}/browse', query: {'path': path}),
        BrowseResult.fromJson,
      );

  /// Starts a restore run; its id. [target] null restores to where the
  /// files came from.
  Future<String> restore(String jobId, {required String versionId, required List<String> paths, BackupEndpoint? target, required String conflict, bool dryRun = false}) => _call(
        () => _c.post('$base/jobs/${Uri.encodeComponent(jobId)}/restore', body: {
          'version_id': versionId,
          'paths': paths,
          'target': target == null ? {'mode': 'original'} : {'mode': 'other', 'endpoint': target.toJson()},
          'conflict': conflict,
          'dry_run': dryRun,
        }),
        (d) => (d as Map?)?['run_id']?.toString() ?? '',
      );

  /// Where a job can read ([role] "source") or write ("dest").
  Future<List<BackupLocation>> locations(String role) =>
      _call(() => _c.get('$base/locations', query: {'role': role}), (d) => [for (final l in d is List ? d : const []) BackupLocation.fromJson(l)]);

  /// The folders in [path] under [endpoint].
  Future<BrowseResult> browseLocation(BackupEndpoint endpoint, String path) => _call(
        () => _c.get('$base/locations/browse', query: {
          'kind': endpoint.kind,
          'ref_id': endpoint.refId,
          if (endpoint.subPath.isNotEmpty) 'sub_path': endpoint.subPath,
          'path': path,
          'dirs_only': '1',
        }),
        BrowseResult.fromJson,
      );
}
