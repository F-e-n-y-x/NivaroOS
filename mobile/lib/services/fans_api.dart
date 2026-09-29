/// Fan control, through the gateway at /v1/fans (nivaroos-fans). Reads
/// need the normal sign-in; changes need an administrator - the server
/// says so in its error message when they don't.
library;

import '../models/fans.dart';
import 'api_client.dart';

class FansApi {
  FansApi([ApiClient? client]) : _c = client ?? ApiClient.instance;

  final ApiClient _c;

  static const base = '/v1/fans';

  /// True when [e] means fan control isn't on this server (an older
  /// NivaroOS without it, or the service not running).
  static bool isUnavailable(Object e) => e is ApiException && (e.statusCode == 404 || e.statusCode == 502 || e.statusCode == 503);

  Future<FansStatus> status() async => FansStatus.fromJson(await _c.get('$base/status'));

  Future<FansStatus> _post(String path, Map<String, Object?> body) async => FansStatus.fromJson(await _c.post('$base$path', body: body));

  Future<FansStatus> updateFan(String id, Map<String, Object?> changes) => _post('/fan', {'id': id, ...changes});

  Future<FansStatus> applyPreset(String preset) => _post('/preset', {'preset': preset});

  Future<FansStatus> allAuto() => _post('/auto', {});

  Future<FansStatus> setCritical({int? cpu, int? gpu}) => _post('/settings', {'critical_cpu_c': ?cpu, 'critical_gpu_c': ?gpu});

  /// Takes about 7 seconds (the fan's speed is changed and watched).
  Future<String> identify(String id) async {
    final res = await _c.post('$base/identify', body: {'id': id}, timeout: const Duration(seconds: 30));
    return (res['message'] as String?) ?? '';
  }
}
