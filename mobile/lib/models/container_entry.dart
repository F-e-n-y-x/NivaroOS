/// One installed compose app, as returned by
/// `GET /v2/app_management/web/appgrid` or `GET /v2/app_management/compose`.
class ComposeApp {
  final String id;
  final String title;
  final String icon;
  final String status;
  final bool updateAvailable;
  final bool isUncontrolled;
  final String appType;
  final String image;
  final String? port;
  final String? scheme;
  final double? iconRadius;
  final String? overrideUrl;

  ComposeApp({
    required this.id,
    required this.title,
    required this.icon,
    required this.status,
    required this.updateAvailable,
    required this.isUncontrolled,
    this.appType = 'v2app',
    this.image = '',
    this.port,
    this.scheme,
    this.iconRadius,
    this.overrideUrl,
  });

  bool get isRunning => status == 'running';

  factory ComposeApp.fromJson(String id, Map<String, dynamic> j) {
    final storeInfo = j['store_info'] as Map<String, dynamic>?;
    final title = _pickLocale(storeInfo?['title'] ?? j['title']) ?? id;
    return ComposeApp(
      id: id,
      title: title,
      icon: storeInfo?['icon']?.toString() ?? (j['icon']?.toString() ?? ''),
      status: j['status']?.toString() ?? 'unknown',
      updateAvailable: j['update_available'] as bool? ?? false,
      isUncontrolled: j['is_uncontrolled'] as bool? ?? false,
      appType: j['app_type']?.toString() ?? 'v2app',
      image: j['image']?.toString() ?? '',
      port: j['port']?.toString(),
      scheme: j['scheme']?.toString(),
    );
  }

  factory ComposeApp.fromGridItem(Map<String, dynamic> j) {
    final name = j['name']?.toString() ?? (j['id']?.toString() ?? 'app');
    final title = _pickLocale(j['title']) ?? name;

    return ComposeApp(
      id: name,
      title: title,
      icon: j['icon']?.toString() ?? '',
      status: j['status']?.toString() ?? 'unknown',
      updateAvailable: false,
      isUncontrolled: j['is_uncontrolled'] as bool? ?? false,
      appType: j['app_type']?.toString() ?? 'v2app',
      image: j['image']?.toString() ?? '',
      port: j['port']?.toString(),
      scheme: j['scheme']?.toString(),
    );
  }

  static String? _pickLocale(dynamic raw) {
    if (raw == null) return null;
    if (raw is String) return raw.isNotEmpty ? raw : null;
    if (raw is Map) {
      final val = raw['custom'] ?? raw['en_us'] ?? raw['en'] ?? (raw.isNotEmpty ? raw.values.first : null);
      return val?.toString();
    }
    return raw.toString();
  }
}

/// A plain Docker container, as returned by `GET /v1/container/all`
class RawContainer {
  final String id;
  final String name;
  final String image;
  final String state;
  final String icon;
  final double? iconRadius;

  RawContainer({
    required this.id,
    required this.name,
    required this.image,
    required this.state,
    this.icon = '',
    this.iconRadius,
  });

  bool get isRunning => state == 'running';

  factory RawContainer.fromJson(Map<String, dynamic> j) => RawContainer(
        id: j['id']?.toString() ?? '',
        name: (j['name']?.toString() ?? '').replaceAll(RegExp(r'^/'), ''),
        image: j['image']?.toString() ?? '',
        state: j['state']?.toString() ?? 'unknown',
        icon: j['icon']?.toString() ?? '',
      );
}

/// One catalog entry from `GET /v2/app_management/apps` (the app store).
class StoreApp {
  final String id;
  final String title;
  final String tagline;
  final String icon;
  final String author;
  final String category;

  StoreApp({
    required this.id,
    required this.title,
    required this.tagline,
    required this.icon,
    required this.author,
    required this.category,
  });

  factory StoreApp.fromJson(String id, Map<String, dynamic> j) {
    final appId = j['id']?.toString() ?? (j['name']?.toString() ?? id);
    return StoreApp(
      id: appId,
      title: ComposeApp._pickLocale(j['title']) ?? (j['name']?.toString() ?? appId),
      tagline: ComposeApp._pickLocale(j['tagline'] ?? j['description']) ?? '',
      icon: j['icon']?.toString() ?? '',
      author: j['author']?.toString() ?? (j['developer']?.toString() ?? ''),
      category: j['category']?.toString() ?? '',
    );
  }
}
