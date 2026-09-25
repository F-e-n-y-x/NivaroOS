/// A cloud drive the server has mounted with rclone (`GET /v1/cloud`).
class CloudAccount {
  final String name;
  final String fs;
  final String mountPoint;
  final String type;
  final String icon;
  final double? uploadMbps;
  final double? downloadMbps;

  const CloudAccount({
    required this.name,
    required this.fs,
    required this.mountPoint,
    required this.type,
    this.icon = '',
    this.uploadMbps,
    this.downloadMbps,
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
    );
  }

  /// Mounted, so its files can be browsed.
  bool get isMounted => mountPoint.isNotEmpty;

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
