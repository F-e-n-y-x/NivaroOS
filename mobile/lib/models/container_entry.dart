// Installed apps and app store entries, parsed from app-management's API
// (services/app-management/api/app_management/openapi.yaml and the v1
// container routes in services/app-management/route/v1.go).

/// Picks the best text from app-management's localised maps
/// (`{"en_US": "Jellyfin", "de_DE": ...}`; the store uses `en_US`, the
/// app grid `en_us`, and a user rename is under `custom`).
String? pickLocale(Object? raw) {
  if (raw == null) return null;
  if (raw is String) return raw.trim().isEmpty ? null : raw;
  if (raw is Map) {
    for (final key in const ['custom', 'en_us', 'en_US', 'en_GB', 'en']) {
      final v = raw[key];
      if (v is String && v.trim().isNotEmpty) return v;
    }
    for (final v in raw.values) {
      if (v is String && v.trim().isNotEmpty) return v;
    }
    return null;
  }
  return raw.toString();
}

/// What sort of thing an installed app is. It decides which routes start,
/// stop, log and update it.
enum AppKind {
  /// A compose app managed by app-management v2 (`/v2/app_management/compose/{id}`).
  compose,

  /// A legacy CasaOS v1 app: one container with app metadata.
  legacy,

  /// A plain Docker container that is not part of a compose app.
  container,

  /// A bookmark to a web address the user added on the dashboard.
  link,
}

/// The user-facing state of an app.
enum AppRunState { running, stopped, starting, stopping, restarting, paused, unknown }

/// start / stop / restart, the body values app-management expects.
enum AppAction { start, stop, restart }

/// Where an app's web interface is, and whether this phone can probably
/// reach it (plan M-13).
class AppAddress {
  const AppAddress(this.url, {this.lanOnly = false});

  final Uri url;

  /// True when the address is the server's own host plus a port, and the
  /// server is reached over the internet (a tunnel or proxy): such ports are
  /// usually not published, so opening it would show a dead page.
  final bool lanOnly;
}

/// True for hosts that only exist on a home network or a tailnet, where
/// every port of the server is reachable: private and link-local IPv4,
/// Tailscale's 100.64/10, loopback, IPv6 ULA/link-local, `.local`, `.lan`,
/// `.home.arpa`, `*.ts.net` and single-label names.
bool isLocalHost(String host) {
  final h = host.toLowerCase().replaceAll(RegExp(r'^\[|\]$'), '');
  if (h.isEmpty) return false;
  if (h == 'localhost' || !h.contains('.') && !h.contains(':')) return true;
  if (h.endsWith('.local') || h.endsWith('.lan') || h.endsWith('.home.arpa') || h.endsWith('.ts.net') || h.endsWith('.internal')) {
    return true;
  }
  final v4 = RegExp(r'^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$').firstMatch(h);
  if (v4 != null) {
    final a = int.parse(v4.group(1)!);
    final b = int.parse(v4.group(2)!);
    return a == 10 ||
        a == 127 ||
        (a == 172 && b >= 16 && b <= 31) ||
        (a == 192 && b == 168) ||
        (a == 169 && b == 254) ||
        (a == 100 && b >= 64 && b <= 127);
  }
  if (h.contains(':')) return h == '::1' || h.startsWith('fd') || h.startsWith('fc') || h.startsWith('fe80');
  return false;
}

/// One installed app, from `GET /v2/app_management/web/appgrid` (the list
/// the web dashboard shows), optionally enriched with the container details
/// from `GET /v1/container/all` (uptime, updates).
class InstalledApp {
  const InstalledApp({
    required this.id,
    required this.title,
    required this.kind,
    this.icon = '',
    this.status = '',
    this.image = '',
    this.scheme,
    this.hostname,
    this.port,
    this.index,
    this.storeAppId,
    this.linkUrl,
    this.overrideUrl,
    this.iconRadius,
    this.containerId,
    this.statusText,
    this.hasUpdate = false,
    this.autoUpdate,
  });

  /// The name every route takes (compose project name, container name, or
  /// the link's name).
  final String id;
  final String title;
  final AppKind kind;
  final String icon;

  /// The raw state from the server: "running", "exited", "created", ...
  final String status;
  final String image;
  final String? scheme;
  final String? hostname;
  final String? port;
  final String? index;
  final String? storeAppId;
  final String? linkUrl;

  /// A web address the user set in the dashboard's "Edit app".
  final String? overrideUrl;
  final double? iconRadius;
  final String? containerId;

  /// Docker's own summary, e.g. "Up 10 hours".
  final String? statusText;
  final bool hasUpdate;
  final bool? autoUpdate;

  static AppKind _kind(String? type) => switch (type) {
        'v2app' => AppKind.compose,
        'v1app' => AppKind.legacy,
        'link' => AppKind.link,
        _ => AppKind.container,
      };

  static String? _str(Object? v) {
    if (v == null) return null;
    final s = v.toString().trim();
    return s.isEmpty ? null : s;
  }

  factory InstalledApp.fromGridItem(Map<String, dynamic> j) {
    final name = _str(j['name']) ?? _str(j['id']) ?? 'app';
    return InstalledApp(
      id: name,
      title: pickLocale(j['title']) ?? name,
      kind: _kind(j['app_type']?.toString()),
      icon: _str(j['icon']) ?? '',
      status: _str(j['status']) ?? '',
      image: _str(j['image']) ?? '',
      scheme: _str(j['scheme']),
      hostname: _str(j['hostname']),
      port: _str(j['port']),
      index: _str(j['index']),
      storeAppId: _str(j['store_app_id']),
    );
  }

  /// A bookmark from the dashboard's `custom/link` setting. The web UI
  /// stores the address as `hostname`; older builds of this app used `url`.
  factory InstalledApp.fromLink(Map<String, dynamic> j) {
    final name = _str(j['name']) ?? 'Link';
    return InstalledApp(
      id: name,
      title: name,
      kind: AppKind.link,
      icon: _str(j['icon']) ?? '',
      linkUrl: _str(j['hostname']) ?? _str(j['url']),
    );
  }

  InstalledApp copyWith({
    String? title,
    String? icon,
    String? status,
    String? overrideUrl,
    double? iconRadius,
    String? containerId,
    String? statusText,
    bool? hasUpdate,
    bool? autoUpdate,
  }) =>
      InstalledApp(
        id: id,
        title: title ?? this.title,
        kind: kind,
        icon: icon ?? this.icon,
        status: status ?? this.status,
        image: image,
        scheme: scheme,
        hostname: hostname,
        port: port,
        index: index,
        storeAppId: storeAppId,
        linkUrl: linkUrl,
        overrideUrl: overrideUrl ?? this.overrideUrl,
        iconRadius: iconRadius ?? this.iconRadius,
        containerId: containerId ?? this.containerId,
        statusText: statusText ?? this.statusText,
        hasUpdate: hasUpdate ?? this.hasUpdate,
        autoUpdate: autoUpdate ?? this.autoUpdate,
      );

  /// The dashboard's "Edit app" overrides (`custom/legacy_app_overrides`,
  /// keyed by app name or title): title, icon, icon radius and web address.
  InstalledApp withOverride(Object? over) {
    if (over is! Map) return this;
    final radius = over['iconRadius'];
    return copyWith(
      title: _str(over['title']),
      icon: _str(over['icon']),
      overrideUrl: _str(over['url']),
      iconRadius: radius is num ? radius.toDouble() : null,
    );
  }

  /// Adds what `/v1/container/all` knows about this app's container.
  InstalledApp withContainer(ContainerInfo c) => copyWith(
        containerId: c.id,
        statusText: c.statusText,
        hasUpdate: c.hasUpdate,
        autoUpdate: c.autoUpdate,
      );

  bool get isLink => kind == AppKind.link;

  AppRunState get runState {
    final s = status.toLowerCase();
    if (isLink) return AppRunState.running;
    if (s.contains('running')) return AppRunState.running;
    if (s.contains('restarting')) return AppRunState.restarting;
    if (s.contains('paused')) return AppRunState.paused;
    if (s.contains('starting')) return AppRunState.starting;
    if (s.contains('exited') || s.contains('stopped') || s.contains('dead') || s == 'created') return AppRunState.stopped;
    return AppRunState.unknown;
  }

  bool get isRunning => runState == AppRunState.running;

  /// Start, stop and restart work for everything but links.
  bool get canControl => !isLink;

  /// Logs and a shell exist for anything backed by containers.
  bool get hasContainer => !isLink;

  /// Edit app works on compose apps: their compose file is what the
  /// server edits (`PUT /v2/app_management/compose/{id}`).
  bool get canEdit => kind == AppKind.compose;

  /// The app's web interface, built the way the web dashboard builds it
  /// (`mixins/app/Business_OpenThirdApp.js`): the user's own address first,
  /// then `scheme://host:port/index` from the app's store info, where host
  /// is the app's own hostname or else the server's. Null when the app has
  /// no web interface this phone knows of (plain containers publish no
  /// address unless the user set one).
  AppAddress? address(String serverBaseUrl) {
    final server = Uri.tryParse(serverBaseUrl.contains('://') ? serverBaseUrl : 'http://$serverBaseUrl');
    Uri? parse(String raw) {
      final u = Uri.tryParse(raw.contains('://') ? raw : 'http://$raw');
      return u == null || u.host.isEmpty ? null : u;
    }

    final custom = overrideUrl ?? (isLink ? linkUrl : null);
    if (custom != null) {
      final u = parse(custom);
      return u == null ? null : AppAddress(u);
    }
    if (kind == AppKind.container || (port == null && hostname == null)) return null;
    final serverHost = server?.host ?? '';
    final host = hostname ?? serverHost;
    if (host.isEmpty) return null;
    var path = index ?? '/';
    if (!path.startsWith('/')) path = '/$path';
    final url = Uri.tryParse('${scheme ?? 'http'}://$host${port == null ? '' : ':$port'}$path');
    if (url == null) return null;
    final onServer = hostname == null || hostname == serverHost;
    return AppAddress(url, lanOnly: port != null && onServer && !isLocalHost(serverHost));
  }

  /// The request that starts, stops or restarts this app. Compose apps
  /// take a bare JSON string on `/status` (openapi.yaml
  /// `RequestComposeAppStatus`; plan M-08); containers and legacy apps take
  /// `{"state": ...}` on the v1 container route, like the web dashboard's
  /// `container.updateState`.
  ({String path, Object body}) statusRequest(AppAction action) => kind == AppKind.compose
      ? (path: '/v2/app_management/compose/${Uri.encodeComponent(id)}/status', body: action.name)
      : (path: '/v1/container/${Uri.encodeComponent(id)}/state', body: {'state': action.name});

  /// The logs request for the last [lines] lines.
  ({String path, Map<String, String> query}) logsRequest(int lines) => kind == AppKind.compose
      ? (path: '/v2/app_management/compose/${Uri.encodeComponent(id)}/logs', query: {'lines': '$lines'})
      : (path: '/v1/container/${Uri.encodeComponent(id)}/logs', query: {'tail': '$lines', 'timestamps': 'true'});

  /// The container shell WebSocket path (wsterm framing). Compose apps
  /// need their main container's id first (`/compose/{id}/containers`).
  String terminalPath([String? container]) => '/v1/container/${Uri.encodeComponent(container ?? containerId ?? id)}/terminal';
}

/// One Docker container from `GET /v1/container/all`.
class ContainerInfo {
  const ContainerInfo({
    required this.id,
    required this.name,
    this.image = '',
    this.state = '',
    this.statusText,
    this.hasUpdate = false,
    this.autoUpdate,
    this.project,
  });

  final String id;
  final String name;
  final String image;
  final String state;
  final String? statusText;
  final bool hasUpdate;
  final bool? autoUpdate;

  /// The compose project this container belongs to, if any.
  final String? project;

  factory ContainerInfo.fromJson(Map<String, dynamic> j) {
    final source = j['source'];
    return ContainerInfo(
      id: j['id']?.toString() ?? '',
      name: (j['name']?.toString() ?? '').replaceFirst(RegExp(r'^/'), ''),
      image: j['image']?.toString() ?? '',
      state: j['state']?.toString() ?? '',
      statusText: InstalledApp._str(j['status']),
      hasUpdate: j['has_update'] == true,
      autoUpdate: j['auto_update_enabled'] is bool ? j['auto_update_enabled'] as bool : null,
      project: source is Map ? InstalledApp._str(source['project']) : null,
    );
  }
}

/// Parses the three answers the Apps tab loads into one sorted list: the
/// app grid, the containers (for uptime and updates) and the dashboard's
/// links and overrides. Containers or overrides that failed to load are
/// passed as empty.
List<InstalledApp> buildInstalledApps({
  required List<dynamic> grid,
  List<dynamic> containers = const [],
  List<dynamic> links = const [],
  Map<String, dynamic> overrides = const {},
}) {
  final byName = <String, ContainerInfo>{};
  for (final c in containers.whereType<Map>()) {
    final info = ContainerInfo.fromJson(Map<String, dynamic>.from(c));
    byName[info.name.toLowerCase()] = info;
  }
  final apps = <InstalledApp>[];
  final seen = <String>{};
  for (final item in grid.whereType<Map>()) {
    var app = InstalledApp.fromGridItem(Map<String, dynamic>.from(item));
    if (!seen.add('${app.kind}:${app.id}')) continue;
    final c = byName[app.id.toLowerCase()];
    if (c != null) app = app.withContainer(c);
    apps.add(app.withOverride(overrides[app.id] ?? overrides[app.title]));
  }
  for (final l in links.whereType<Map>()) {
    final app = InstalledApp.fromLink(Map<String, dynamic>.from(l));
    if (!seen.add('link:${app.id}')) continue;
    apps.add(app.withOverride(overrides[app.id]));
  }
  apps.sort((a, b) => a.title.toLowerCase().compareTo(b.title.toLowerCase()));
  return apps;
}

/// Server CPU architectures as the store lists them (`amd64`, `arm64`, ...),
/// from whatever the server reports.
String normalizeArch(String raw) => switch (raw.toLowerCase().trim()) {
      'x86_64' || 'x64' || 'amd64' => 'amd64',
      'aarch64' || 'arm64' || 'armv8' => 'arm64',
      'armv7' || 'armv7l' || 'armhf' || 'arm' => 'arm',
      'i386' || 'i686' || '386' => '386',
      final other => other,
    };

/// One app in the store catalogue (`GET /v2/app_management/apps` list
/// entry, or `GET /v2/app_management/apps/{id}`).
class StoreApp {
  const StoreApp({
    required this.id,
    required this.title,
    this.tagline = '',
    this.description = '',
    this.icon = '',
    this.thumbnail = '',
    this.screenshots = const [],
    this.author = '',
    this.developer = '',
    this.category = '',
    this.architectures = const [],
    this.portMap,
    this.tips,
    this.image,
  });

  final String id;
  final String title;
  final String tagline;
  final String description;
  final String icon;
  final String thumbnail;
  final List<String> screenshots;
  final String author;
  final String developer;
  final String category;

  /// CPUs the app runs on; empty means the store doesn't say.
  final List<String> architectures;
  final String? portMap;

  /// What the store wants the user to read before installing.
  final String? tips;

  /// The main service's image, e.g. "linuxserver/jellyfin:10.11.10".
  final String? image;

  /// Kept for older callers: the author or the developer.
  String get by => developer.isNotEmpty ? developer : author;

  /// False when the store lists architectures and [arch] isn't one of them.
  bool supports(String? arch) {
    if (arch == null || arch.isEmpty || architectures.isEmpty) return true;
    final a = normalizeArch(arch);
    return architectures.any((x) => normalizeArch(x) == a);
  }

  factory StoreApp.fromJson(String id, Map<String, dynamic> j) {
    final shots = j['screenshot_link'];
    final tips = j['tips'];
    final apps = j['apps'];
    final main = j['main']?.toString();
    String? image;
    if (apps is Map) {
      final m = apps[main] ?? (apps.isNotEmpty ? apps.values.first : null);
      if (m is Map) image = InstalledApp._str(m['image']);
    }
    String? tip;
    if (tips is Map) tip = pickLocale(tips['custom']) ?? pickLocale(tips['before_install']);
    return StoreApp(
      id: id,
      title: pickLocale(j['title']) ?? InstalledApp._str(j['name']) ?? id,
      tagline: pickLocale(j['tagline']) ?? '',
      description: pickLocale(j['description']) ?? '',
      icon: j['icon']?.toString() ?? '',
      thumbnail: j['thumbnail']?.toString() ?? '',
      screenshots: shots is List ? shots.map((e) => e.toString()).where((s) => s.isNotEmpty).toList() : const [],
      author: j['author']?.toString() ?? '',
      developer: j['developer']?.toString() ?? '',
      category: j['category']?.toString() ?? '',
      architectures: j['architectures'] is List ? (j['architectures'] as List).map((e) => e.toString()).toList() : const [],
      portMap: InstalledApp._str(j['port_map']),
      tips: (tip == null || tip.trim().isEmpty) ? null : tip.trim(),
      image: image,
    );
  }
}

/// `GET /v2/app_management/apps`: `{"data": {"list": {id: info}, "installed": [id]}}`.
class StoreCatalog {
  const StoreCatalog({required this.apps, required this.installed});

  final List<StoreApp> apps;
  final Set<String> installed;

  factory StoreCatalog.fromResponse(Map<String, dynamic> res) {
    final data = res['data'];
    final apps = <StoreApp>[];
    final installed = <String>{};
    if (data is Map) {
      final list = data['list'];
      if (list is Map) {
        for (final e in list.entries) {
          if (e.value is Map) apps.add(StoreApp.fromJson(e.key.toString(), Map<String, dynamic>.from(e.value as Map)));
        }
      }
      final inst = data['installed'];
      if (inst is List) installed.addAll(inst.map((e) => e.toString()));
    }
    apps.sort((a, b) => a.title.toLowerCase().compareTo(b.title.toLowerCase()));
    return StoreCatalog(apps: apps, installed: installed);
  }
}

/// A store category (`GET /v2/app_management/categories`).
class StoreCategory {
  const StoreCategory(this.name, this.count);

  final String name;
  final int count;

  static List<StoreCategory> listFrom(Map<String, dynamic> res) {
    final data = res['data'];
    if (data is! List) return const [];
    return [
      for (final c in data.whereType<Map>())
        if (c['name'] != null && c['name'] != 'All' && ((c['count'] as num?) ?? 0) > 0)
          StoreCategory(c['name'].toString(), (c['count'] as num).toInt()),
    ];
  }
}

/// An app-management event from the message bus
/// (`/v2/message_bus/event/app-management`): `{"name": "app:install-progress",
/// "properties": {"app:name": "jellyfin", "app:progress": "64"}}`.
class AppEvent {
  const AppEvent(this.name, this.properties);

  final String name;
  final Map<String, String> properties;

  /// The app the event is about.
  String? get app => properties['app:name'] ?? properties['name'];

  /// Install progress 0-100, when the event carries one.
  int? get progress => int.tryParse(properties['app:progress'] ?? properties['progress'] ?? '');

  String? get message => properties['message'];

  static AppEvent? tryParse(Object? json) {
    if (json is! Map) return null;
    final name = json['name']?.toString();
    final props = json['properties'] ?? json['Properties'];
    if (name == null) return null;
    return AppEvent(name, {
      if (props is Map)
        for (final e in props.entries) e.key.toString(): e.value?.toString() ?? '',
    });
  }
}
