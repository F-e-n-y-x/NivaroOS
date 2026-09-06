class CloudAccount {
  final String name;
  final String fs;
  final String mountPoint;
  final String type;
  final String icon;
  final double? uploadMbps;
  final double? downloadMbps;

  CloudAccount({
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
    final speed = j['speed_results'] as Map<String, dynamic>?;
    if (speed != null) {
      if (speed['upload_mbps'] is num) up = (speed['upload_mbps'] as num).toDouble();
      if (speed['download_mbps'] is num) down = (speed['download_mbps'] as num).toDouble();
    }
    return CloudAccount(
      name: j['name'] as String? ?? j['fs'] as String? ?? 'Cloud Account',
      fs: j['fs'] as String? ?? '',
      mountPoint: j['mount_point'] as String? ?? '',
      type: j['type'] as String? ?? 'cloud',
      icon: j['icon'] as String? ?? '',
      uploadMbps: up,
      downloadMbps: down,
    );
  }

  String get displayName {
    if (name.isNotEmpty) return name;
    if (fs.isNotEmpty) return fs.replaceAll(':', '');
    return type.toUpperCase();
  }

  String get providerTitle {
    switch (type.toLowerCase()) {
      case 'drive':
        return 'Google Drive';
      case 'onedrive':
        return 'Microsoft OneDrive';
      case 'dropbox':
        return 'Dropbox';
      case 'icloud':
        return 'Apple iCloud';
      case 'nextcloud':
        return 'Nextcloud';
      case 's3':
        return 'Amazon S3 / MinIO';
      case 'webdav':
        return 'WebDAV Storage';
      case 'mega':
        return 'MEGA Cloud';
      default:
        return '${type[0].toUpperCase()}${type.substring(1)} Cloud';
    }
  }
}
