/// One installed compose app, as returned by
/// `GET /v2/app_management/compose` (a map keyed by app id, each value
/// shaped like Go's `ComposeAppWithStoreInfo`).
class ComposeApp {
  final String id;
  final String title;
  final String icon;
  final String status;
  final bool updateAvailable;
  final bool isUncontrolled;

  ComposeApp({
    required this.id,
    required this.title,
    required this.icon,
    required this.status,
    required this.updateAvailable,
    required this.isUncontrolled,
  });

  bool get isRunning => status == 'running';

  factory ComposeApp.fromJson(String id, Map<String, dynamic> j) {
    final storeInfo = j['store_info'] as Map<String, dynamic>?;
    final titleMap = storeInfo?['title'] as Map<String, dynamic>?;
    final title = _pickLocale(titleMap) ?? id;
    return ComposeApp(
      id: id,
      title: title,
      icon: storeInfo?['icon'] as String? ?? '',
      status: j['status'] as String? ?? 'unknown',
      updateAvailable: j['update_available'] as bool? ?? false,
      isUncontrolled: j['is_uncontrolled'] as bool? ?? false,
    );
  }

  static String? _pickLocale(Map<String, dynamic>? map) {
    if (map == null || map.isEmpty) return null;
    return (map['en_us'] ?? map['en'] ?? map.values.first)?.toString();
  }
}

/// A plain Docker container, as returned by `GET /v1/container/all` - the
/// fallback source for anything NOT installed through NivaroOS's own
/// compose/app-management system (e.g. something installed via Portainer,
/// or a bare `docker run`). `GET /v2/app_management/compose` only knows
/// about compose-managed apps, so relying on it alone would make any
/// container installed another way silently disappear from the app.
class RawContainer {
  final String id;
  final String name;
  final String image;
  final String state;

  RawContainer({required this.id, required this.name, required this.image, required this.state});

  bool get isRunning => state == 'running';

  factory RawContainer.fromJson(Map<String, dynamic> j) => RawContainer(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        image: j['image'] as String? ?? '',
        state: j['state'] as String? ?? 'unknown',
      );
}

/// One catalog entry from `GET /v2/app_management/apps` (the app store),
/// shaped like Go's `ComposeAppStoreInfo`.
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
    return StoreApp(
      id: id,
      title: ComposeApp._pickLocale(j['title'] as Map<String, dynamic>?) ?? id,
      tagline: ComposeApp._pickLocale(j['tagline'] as Map<String, dynamic>?) ?? '',
      icon: j['icon'] as String? ?? '',
      author: j['author'] as String? ?? '',
      category: j['category'] as String? ?? '',
    );
  }
}
