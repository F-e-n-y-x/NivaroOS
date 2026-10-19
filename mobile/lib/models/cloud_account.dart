/// A cloud drive the server has mounted with rclone (`GET /v1/cloud`).
class CloudAccount {
  final String name;
  final String fs;
  final String mountPoint;
  final String type;
  final String icon;
  final double? uploadMbps;
  final double? downloadMbps;

  /// "" mounted, "not_mounted", or "sign_in_expired" (Reconnect fixes
  /// it); [problem] says why.
  final String status;
  final String problem;

  /// Its OAuth app's (public) client ID when it signs in with the user's
  /// own app; empty for rclone's shared one.
  final String clientId;

  const CloudAccount({
    required this.name,
    required this.fs,
    required this.mountPoint,
    required this.type,
    this.icon = '',
    this.uploadMbps,
    this.downloadMbps,
    this.status = '',
    this.problem = '',
    this.clientId = '',
  });

  factory CloudAccount.fromJson(Map<String, dynamic> j) {
    double? up;
    double? down;
    final speed = j['speed_results'];
    if (speed is Map) {
      if (speed['upload_mbps'] is num) up = (speed['upload_mbps'] as num).toDouble();
      if (speed['download_mbps'] is num) down = (speed['download_mbps'] as num).toDouble();
    }
    final name = j['name'] as String? ?? '';
    return CloudAccount(
      name: name,
      fs: j['fs'] as String? ?? '',
      mountPoint: j['mount_point'] as String? ?? '',
      type: j['type'] as String? ?? '',
      icon: j['icon'] as String? ?? '',
      uploadMbps: up,
      downloadMbps: down,
      status: j['status'] as String? ?? '',
      problem: j['problem'] as String? ?? '',
      clientId: j['client_id'] as String? ?? '',
    );
  }

  /// Mounted, so its files can be browsed.
  bool get isMounted => mountPoint.isNotEmpty && status.isEmpty;

  /// The provider no longer accepts the sign-in: someone has to Reconnect.
  bool get signInExpired => status == 'sign_in_expired';

  /// The rclone remote ("gdrive"), which the server's account routes take.
  String get remote => fs.endsWith(':') ? fs.substring(0, fs.length - 1) : fs;

  /// Signs in through the browser (Reconnect works from the app).
  bool get browserSignIn => const {'drive', 'onedrive', 'dropbox'}.contains(type);

  String get displayName {
    if (name.isNotEmpty) return name;
    if (fs.isNotEmpty) return fs.replaceAll(':', '');
    return providerTitle;
  }

  /// The service's own name ("Google Drive"), or "Cloud drive" when the
  /// type is unknown.
  String get providerTitle {
    switch (type.toLowerCase()) {
      case 'drive':
        return 'Google Drive';
      case 'onedrive':
        return 'OneDrive';
      case 'dropbox':
        return 'Dropbox';
      case 'icloud':
        return 'iCloud';
      case 'nextcloud':
        return 'Nextcloud';
      case 's3':
        return 'S3 storage';
      case 'webdav':
        return 'WebDAV';
      case 'mega':
        return 'MEGA';
      case '':
        return 'Cloud drive';
      default:
        return type;
    }
  }
}
