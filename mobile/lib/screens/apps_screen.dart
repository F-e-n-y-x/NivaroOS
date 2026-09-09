import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/storage_service.dart';
import '../models/container_entry.dart';
import '../widgets/common.dart';
import '../utils/app_icons.dart';
import 'app_store_screen.dart';
import 'container_logs_screen.dart';
import 'terminal_screen.dart';
import 'login_screen.dart';

class AppsScreen extends StatefulWidget {
  const AppsScreen({super.key});

  @override
  State<AppsScreen> createState() => _AppsScreenState();
}

class _AppsScreenState extends State<AppsScreen> {
  List<ComposeApp> _apps = [];
  List<RawContainer> _others = [];
  bool _loading = true;
  String? _error;
  bool _isGridView = true;
  String _filter = 'all';
  final Set<String> _busy = {};
  final _searchController = TextEditingController();

  static List<ComposeApp> get _builtInApps => [
        ComposeApp(
          id: 'appstore',
          title: 'App Store',
          icon: 'assets/app/appstore.png',
          status: 'running',
          updateAvailable: false,
          isUncontrolled: false,
          appType: 'system',
        ),
        ComposeApp(
          id: 'files',
          title: 'Files',
          icon: 'assets/app/files.svg',
          status: 'running',
          updateAvailable: false,
          isUncontrolled: false,
          appType: 'system',
        ),
        ComposeApp(
          id: 'settings',
          title: 'Settings',
          icon: 'assets/app/settings.png',
          status: 'running',
          updateAvailable: false,
          isUncontrolled: false,
          appType: 'system',
        ),
        ComposeApp(
          id: 'terminal',
          title: 'Terminal',
          icon: 'assets/app/terminal.png',
          status: 'running',
          updateAvailable: false,
          isUncontrolled: false,
          appType: 'system',
        ),
        ComposeApp(
          id: 'vms',
          title: 'VMs',
          icon: 'assets/app/vm-manager.png',
          status: 'running',
          updateAvailable: false,
          isUncontrolled: false,
          appType: 'system',
        ),
      ];

  @override
  void initState() {
    super.initState();
    _load();
    _searchController.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  List<ComposeApp> get _filteredApps {
    final query = _searchController.text.trim().toLowerCase();
    var list = _apps;
    if (_filter == 'running') {
      list = list.where((a) => a.isRunning).toList();
    } else if (_filter == 'stopped') {
      list = list.where((a) => !a.isRunning).toList();
    } else if (_filter == 'system') {
      list = list.where((a) => a.appType == 'system').toList();
    }

    if (query.isNotEmpty) {
      list = list.where((a) => a.title.toLowerCase().contains(query) || a.id.toLowerCase().contains(query)).toList();
    }
    return list;
  }

  List<RawContainer> get _filteredOthers {
    if (_filter == 'running') {
      return _others.where((c) => c.isRunning).toList();
    } else if (_filter == 'stopped') {
      return _others.where((c) => !c.isRunning).toList();
    } else if (_filter == 'system') {
      return [];
    } else if (_filter == 'all') {
      final query = _searchController.text.trim().toLowerCase();
      if (query.isEmpty) return _others;
      return _others.where((c) => c.name.toLowerCase().contains(query)).toList();
    }
    return [];
  }

  Future<void> _promptReauth() async {
    final username = await StorageService.instance.getUsername();
    if (!mounted) return;
    final success = await Navigator.of(context).push<bool>(
      MaterialPageRoute(
        builder: (_) => LoginScreen(
          isReauth: true,
          initialUsername: username,
        ),
      ),
    );
    if (success == true && mounted) {
      _load();
    }
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      List<ComposeApp> apps = [];

      // 1. Fetch WebAppGrid / Compose Apps (same as WebUI AppSection.vue)
      try {
        final gridRes = await ApiClient.instance.get('/v2/app_management/web/appgrid');
        final gridData = gridRes['data'] as List<dynamic>? ?? [];
        apps = gridData.map((e) => ComposeApp.fromGridItem(e as Map<String, dynamic>)).toList();
      } catch (_) {
        try {
          final composeRes = await ApiClient.instance.get('/v2/app_management/compose');
          final composeData = composeRes['data'] as Map<String, dynamic>? ?? {};
          apps = composeData.entries.map((e) => ComposeApp.fromJson(e.key, e.value as Map<String, dynamic>)).toList();
        } catch (_) {}
      }

      // 2. Fetch WebUI custom display overrides (e.g. custom icon/title/radius set in WebUI "Edit App")
      Map<String, dynamic> overrides = {};
      try {
        final overRes = await ApiClient.instance.get('/users/current/custom/legacy_app_overrides');
        dynamic overData = overRes['data'];
        if (overData is String && overData.isNotEmpty) {
          overData = jsonDecode(overData);
        }
        if (overData is Map<String, dynamic>) {
          overrides = overData;
        }
      } catch (_) {}

      // 3. Fetch custom WebUI link apps
      List<ComposeApp> linkApps = [];
      try {
        final linkRes = await ApiClient.instance.get('/users/current/custom/link');
        dynamic linkData = linkRes['data'];
        if (linkData is String && linkData.isNotEmpty) {
          linkData = jsonDecode(linkData);
        }
        if (linkData is List) {
          linkApps = linkData.whereType<Map<String, dynamic>>().map((item) {
            final name = item['name'] as String? ?? 'Link App';
            return ComposeApp(
              id: name,
              title: name,
              icon: item['icon'] as String? ?? '',
              status: 'running',
              updateAvailable: false,
              isUncontrolled: false,
              appType: 'link',
              scheme: item['url'] as String?,
            );
          }).toList();
        }
      } catch (_) {}

      // 4. Combine Built-in + Installed + Links
      final allApps = <ComposeApp>[
        ..._builtInApps,
        ...apps,
        ...linkApps,
      ];

      // 5. Apply WebUI overrides (exact parity with WebUI AppSection.vue)
      final resolvedApps = allApps.map((app) {
        final over = overrides[app.id] ?? overrides[app.title];
        if (over is Map<String, dynamic>) {
          final customIcon = over['icon'] as String?;
          final customTitle = over['title'] as String?;
          final customUrl = over['url'] as String?;
          final customRadius = over['iconRadius'] != null ? (over['iconRadius'] as num).toDouble() : null;
          return ComposeApp(
            id: app.id,
            title: customTitle != null && customTitle.isNotEmpty ? customTitle : app.title,
            icon: customIcon != null && customIcon.isNotEmpty ? customIcon : app.icon,
            status: app.status,
            updateAvailable: app.updateAvailable,
            isUncontrolled: app.isUncontrolled,
            appType: app.appType,
            image: app.image,
            port: app.port,
            scheme: app.scheme,
            iconRadius: customRadius ?? app.iconRadius,
            overrideUrl: customUrl ?? app.overrideUrl,
          );
        }
        return app;
      }).toList();

      resolvedApps.sort((a, b) => a.title.toLowerCase().compareTo(b.title.toLowerCase()));

      // 6. Fetch raw standalone containers
      var others = <RawContainer>[];
      try {
        final containerRes = await ApiClient.instance.get('/container/all');
        final containerData = containerRes['data'] as List<dynamic>? ?? [];
        final containers = containerData.map((e) => RawContainer.fromJson(e as Map<String, dynamic>)).toList();
        final composeIds = resolvedApps.map((a) => a.id.toLowerCase()).toList();
        others = containers.where((c) {
          final name = c.name.toLowerCase();
          return !composeIds.contains(name) && !composeIds.contains(c.id.toLowerCase());
        }).map((c) {
          final over = overrides[c.name] ?? overrides[c.id];
          if (over is Map<String, dynamic>) {
            final customIcon = over['icon'] as String?;
            final customRadius = over['iconRadius'] != null ? (over['iconRadius'] as num).toDouble() : null;
            return RawContainer(
              id: c.id,
              name: over['title'] as String? ?? c.name,
              image: c.image,
              state: c.state,
              icon: customIcon ?? c.icon,
              iconRadius: customRadius,
            );
          }
          return c;
        }).toList();
      } catch (_) {}

      if (!mounted) return;
      setState(() {
        _apps = resolvedApps;
        _others = others;
        _loading = false;
        _error = null;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = e.toString().replaceFirst('Exception: ', '');
      });
    }
  }

  String _getAppBrowserUrl(ComposeApp app) {
    if (app.overrideUrl != null && app.overrideUrl!.isNotEmpty) {
      return app.overrideUrl!;
    }
    if (app.scheme != null && app.scheme!.isNotEmpty) {
      return app.scheme!;
    }
    if (app.port != null && app.port!.isNotEmpty) {
      final host = Uri.parse(ApiClient.instance.baseUrl).host;
      return 'http://$host:${app.port}';
    }
    return '';
  }

  Future<void> _launchInBrowser(String urlStr) async {
    if (urlStr.isEmpty) return;
    try {
      final uri = Uri.parse(urlStr);
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not open browser: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    }
  }

  Future<void> _openApp(ComposeApp app) async {
    if (app.appType == 'system') {
      _openSystemApp(app.id);
      return;
    }

    final browserUrl = _getAppBrowserUrl(app);
    if (browserUrl.isNotEmpty) {
      _showAppActionsModal(app);
    } else {
      _showAppDetails(app);
    }
  }

  void _showAppActionsModal(ComposeApp app) {
    final browserUrl = _getAppBrowserUrl(app);
    showModalBottomSheet(
      context: context,
      backgroundColor: Colors.transparent,
      builder: (context) => Container(
        padding: const EdgeInsets.all(22),
        decoration: const BoxDecoration(
          color: NivaroColors.surfaceContainerLowest,
          borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                NivaroAppIcon(
                  iconUrl: app.icon,
                  name: app.title,
                  size: 48,
                  radius: 12,
                  customRadiusPercent: app.iconRadius,
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(app.title, style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16.5, color: NivaroColors.textPrimary)),
                      const SizedBox(height: 2),
                      Row(
                        children: [
                          PulsingStatusDot(color: app.isRunning ? NivaroColors.success : NivaroColors.textMuted, size: 6),
                          const SizedBox(width: 6),
                          Text(
                            app.isRunning ? 'RUNNING' : 'STOPPED',
                            style: TextStyle(
                              color: app.isRunning ? NivaroColors.successLight : NivaroColors.textMuted,
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          if (app.port != null && app.port!.isNotEmpty) ...[
                            const SizedBox(width: 8),
                            Text('· Port ${app.port}', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11)),
                          ],
                        ],
                      ),
                    ],
                  ),
                ),
                RoundIconButton(icon: Icons.close_rounded, size: 34, onPressed: () => Navigator.pop(context)),
              ],
            ),
            const SizedBox(height: 16),
            const Divider(height: 1, color: NivaroColors.borderSubtle),
            const SizedBox(height: 8),

            if (browserUrl.isNotEmpty)
              ListTile(
                leading: const Icon(Icons.open_in_browser_rounded, color: NivaroColors.primaryLight),
                title: const Text('Open in Browser', style: TextStyle(fontWeight: FontWeight.w700)),
                subtitle: Text(browserUrl, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11), maxLines: 1, overflow: TextOverflow.ellipsis),
                trailing: const Icon(Icons.arrow_outward_rounded, size: 18, color: NivaroColors.textFaint),
                onTap: () {
                  Navigator.pop(context);
                  _launchInBrowser(browserUrl);
                },
              ),

            if (app.appType != 'system') ...[
              ListTile(
                leading: const Icon(Icons.terminal_rounded, color: NivaroColors.warningLight),
                title: const Text('Open Container Terminal', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Interactive root shell inside container', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11)),
                onTap: () {
                  Navigator.pop(context);
                  Navigator.of(context).push(
                    MaterialPageRoute(
                      builder: (_) => TerminalScreen(
                        initCommand: 'docker exec -it ${app.id} /bin/sh || docker exec -it ${app.id} /bin/bash',
                        title: '${app.title} Shell',
                      ),
                    ),
                  );
                },
              ),
              ListTile(
                leading: const Icon(Icons.article_rounded, color: NivaroColors.infoLight),
                title: const Text('Container Logs', style: TextStyle(fontWeight: FontWeight.w600)),
                subtitle: const Text('Stream real-time STDOUT & STDERR logs', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11)),
                onTap: () {
                  Navigator.pop(context);
                  Navigator.of(context).push(MaterialPageRoute(builder: (_) => ContainerLogsScreen(appId: app.id, appTitle: app.title)));
                },
              ),
              ListTile(
                leading: Icon(app.isRunning ? Icons.stop_circle_outlined : Icons.play_circle_outline_rounded, color: app.isRunning ? NivaroColors.dangerLight : NivaroColors.successLight),
                title: Text(app.isRunning ? 'Stop App' : 'Start App', style: const TextStyle(fontWeight: FontWeight.w600)),
                onTap: () {
                  Navigator.pop(context);
                  _toggleApp(app);
                },
              ),
              ListTile(
                leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.purpleLight),
                title: const Text('Restart App Container', style: TextStyle(fontWeight: FontWeight.w600)),
                onTap: () {
                  Navigator.pop(context);
                  _restartApp(app);
                },
              ),
            ],
          ],
        ),
      ),
    );
  }

  void _openSystemApp(String id) {
    if (id == 'appstore') {
      Navigator.of(context).push(MaterialPageRoute(builder: (_) => const AppStoreScreen())).then((_) => _load());
    } else if (id == 'terminal') {
      Navigator.of(context).push(MaterialPageRoute(builder: (_) => const TerminalScreen()));
    } else {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Access ${id.toUpperCase()} from main navigation.')),
      );
    }
  }

  Future<void> _toggleApp(ComposeApp app) async {
    if (_busy.contains(app.id)) return;
    setState(() => _busy.add(app.id));
    final action = app.isRunning ? 'stop' : 'start';
    try {
      await ApiClient.instance.put('/v2/app_management/compose/${app.id}/state', body: {'state': action});
      await Future.delayed(const Duration(milliseconds: 600));
      await _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    } finally {
      if (mounted) setState(() => _busy.remove(app.id));
    }
  }

  Future<void> _restartApp(ComposeApp app) async {
    if (_busy.contains(app.id)) return;
    setState(() => _busy.add(app.id));
    try {
      await ApiClient.instance.put('/v2/app_management/compose/${app.id}/state', body: {'state': 'restart'});
      await Future.delayed(const Duration(milliseconds: 600));
      await _load();
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Failed: $e'), backgroundColor: NivaroColors.danger));
      }
    } finally {
      if (mounted) setState(() => _busy.remove(app.id));
    }
  }

  void _showAppDetails(ComposeApp app) {
    showModalBottomSheet(
      context: context,
      backgroundColor: Colors.transparent,
      builder: (context) => Container(
        padding: const EdgeInsets.all(24),
        decoration: const BoxDecoration(
          color: NivaroColors.surfaceContainerLowest,
          borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                NivaroAppIcon(
                  iconUrl: app.icon,
                  name: app.title,
                  size: 52,
                  radius: 14,
                  customRadiusPercent: app.iconRadius,
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(app.title, style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: NivaroColors.textPrimary)),
                      const SizedBox(height: 2),
                      Row(
                        children: [
                          PulsingStatusDot(color: app.isRunning ? NivaroColors.success : NivaroColors.textMuted, size: 6),
                          const SizedBox(width: 6),
                          Text(
                            app.isRunning ? 'RUNNING' : 'STOPPED',
                            style: TextStyle(
                              color: app.isRunning ? NivaroColors.successLight : NivaroColors.textMuted,
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                          if (app.port != null && app.port!.isNotEmpty) ...[
                            const SizedBox(width: 8),
                            Text('· Port ${app.port}', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11)),
                          ],
                        ],
                      ),
                    ],
                  ),
                ),
                RoundIconButton(icon: Icons.close_rounded, size: 36, onPressed: () => Navigator.pop(context)),
              ],
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                if (app.appType != 'system') ...[
                  Expanded(
                    child: OutlinedButton.icon(
                      icon: Icon(app.isRunning ? Icons.stop_rounded : Icons.play_arrow_rounded),
                      label: Text(app.isRunning ? 'Stop' : 'Start'),
                      onPressed: () {
                        Navigator.pop(context);
                        _toggleApp(app);
                      },
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: OutlinedButton.icon(
                      icon: const Icon(Icons.restart_alt_rounded),
                      label: const Text('Restart'),
                      onPressed: () {
                        Navigator.pop(context);
                        _restartApp(app);
                      },
                    ),
                  ),
                ],
              ],
            ),
            if (app.appType != 'system') ...[
              const SizedBox(height: 10),
              ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.article_rounded, color: NivaroColors.primaryLight),
                title: const Text('Container Logs', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 14)),
                trailing: const Icon(Icons.chevron_right_rounded, color: NivaroColors.textFaint),
                onTap: () {
                  Navigator.pop(context);
                  Navigator.of(context).push(MaterialPageRoute(builder: (_) => ContainerLogsScreen(appId: app.id, appTitle: app.title)));
                },
              ),
            ],
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      bottom: false,
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: RefreshIndicator(
          onRefresh: _load,
          color: NivaroColors.primaryLight,
          backgroundColor: NivaroColors.surfaceRaised,
          child: Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 1150),
              child: ListView(
                padding: const EdgeInsets.fromLTRB(20, 14, 20, 140),
            children: [
              // Header
              Row(
                children: [
                  const Expanded(
                    child: Text(
                      'Applications',
                      style: TextStyle(fontWeight: FontWeight.w800, fontSize: 22, color: NivaroColors.textPrimary),
                    ),
                  ),
                  RoundIconButton(
                    icon: _isGridView ? Icons.view_list_rounded : Icons.grid_view_rounded,
                    tooltip: _isGridView ? 'List View' : 'Grid View',
                    onPressed: () => setState(() => _isGridView = !_isGridView),
                  ),
                  const SizedBox(width: 8),
                  RoundIconButton(
                    icon: Icons.storefront_rounded,
                    tooltip: 'App Store',
                    color: NivaroColors.primary,
                    iconColor: Colors.white,
                    onPressed: () {
                      Navigator.of(context).push(MaterialPageRoute(builder: (_) => const AppStoreScreen())).then((_) => _load());
                    },
                  ),
                ],
              ),
              const SizedBox(height: 16),

              // Search Bar
              TextField(
                controller: _searchController,
                style: const TextStyle(fontSize: 14),
                decoration: InputDecoration(
                  hintText: 'Search installed applications...',
                  prefixIcon: const Icon(Icons.search_rounded, size: 20, color: NivaroColors.textMuted),
                  suffixIcon: _searchController.text.isNotEmpty
                      ? IconButton(
                          icon: const Icon(Icons.clear_rounded, size: 18),
                          onPressed: () => _searchController.clear(),
                        )
                      : null,
                ),
              ),
              const SizedBox(height: 12),

              // Filter Chips
              SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: Row(
                  children: [
                    _FilterChip(label: 'All (${_apps.length})', selected: _filter == 'all', onSelected: () => setState(() => _filter = 'all')),
                    const SizedBox(width: 8),
                    _FilterChip(label: 'Running (${_apps.where((a) => a.isRunning).length})', selected: _filter == 'running', onSelected: () => setState(() => _filter = 'running')),
                    const SizedBox(width: 8),
                    _FilterChip(label: 'Stopped (${_apps.where((a) => !a.isRunning).length})', selected: _filter == 'stopped', onSelected: () => setState(() => _filter = 'stopped')),
                    const SizedBox(width: 8),
                    _FilterChip(label: 'System (${_apps.where((a) => a.appType == 'system').length})', selected: _filter == 'system', onSelected: () => setState(() => _filter = 'system')),
                  ],
                ),
              ),
              const SizedBox(height: 20),

              if (_loading)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 40),
                  child: Center(child: CircularProgressIndicator()),
                )
              else if (_error != null)
                DarkCard(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    children: [
                      const Icon(Icons.error_outline_rounded, color: NivaroColors.dangerLight, size: 36),
                      const SizedBox(height: 10),
                      Text(_error!, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                      const SizedBox(height: 14),
                      if (ApiClient.isAuthError(_error)) ...[
                        FilledButton.icon(
                          onPressed: _promptReauth,
                          icon: const Icon(Icons.lock_open_rounded, size: 18, color: Colors.white),
                          label: const Text('Sign In Again', style: TextStyle(color: Colors.white, fontWeight: FontWeight.w700)),
                          style: FilledButton.styleFrom(
                            backgroundColor: NivaroColors.primary,
                            foregroundColor: Colors.white,
                            padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 12),
                            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                          ),
                        ),
                        const SizedBox(height: 8),
                        TextButton(
                          onPressed: _load,
                          child: const Text('Retry Connection', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12.5)),
                        ),
                      ] else
                        OutlinedButton(onPressed: _load, child: const Text('Retry')),
                    ],
                  ),
                )
              else if (_filteredApps.isEmpty && _filteredOthers.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 40),
                  child: Center(child: Text('No applications found.', style: TextStyle(color: NivaroColors.textMuted))),
                )
              else ...[
                Builder(builder: (context) {
                  final width = MediaQuery.of(context).size.width;
                  final isLandscape = MediaQuery.of(context).orientation == Orientation.landscape;

                  if (_isGridView) {
                    final cols = width >= 1200 ? 9 : (width >= 900 ? 7 : (width >= 600 || isLandscape ? 6 : (width >= 400 ? 4 : 3)));
                    final ratio = width >= 600 ? 0.92 : 0.80;

                    return GridView.builder(
                      shrinkWrap: true,
                      physics: const NeverScrollableScrollPhysics(),
                      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                        crossAxisCount: cols,
                        mainAxisSpacing: 14,
                        crossAxisSpacing: 10,
                        childAspectRatio: ratio,
                      ),
                      itemCount: _filteredApps.length,
                      itemBuilder: (context, index) {
                        final app = _filteredApps[index];
                        return AppTile(
                          name: app.title,
                          iconUrl: app.icon,
                          imageName: app.image,
                          running: app.isRunning,
                          customRadiusPercent: app.iconRadius,
                          onTap: () => _openApp(app),
                          onLongPress: () => _showAppDetails(app),
                        );
                      },
                    );
                  }

                  // List View Mode (2 columns on tablet)
                  final cols = width >= 750 ? 2 : 1;
                  if (cols > 1) {
                    return GridView.builder(
                      shrinkWrap: true,
                      physics: const NeverScrollableScrollPhysics(),
                      gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                        crossAxisCount: 2,
                        crossAxisSpacing: 10,
                        mainAxisSpacing: 10,
                        childAspectRatio: 3.8,
                      ),
                      itemCount: _filteredApps.length,
                      itemBuilder: (context, index) => _buildAppListItem(_filteredApps[index]),
                    );
                  }

                  return Column(
                    children: _filteredApps.map((app) => Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: _buildAppListItem(app),
                        )).toList(),
                  );
                }),

                // Standalone Containers Section (if any)
                if (_filteredOthers.isNotEmpty) ...[
                  const SizedBox(height: 24),
                  const SectionHeader(
                    title: 'Standalone Containers',
                    subtitle: 'Docker containers running outside compose',
                  ),
                  Builder(builder: (context) {
                    final width = MediaQuery.of(context).size.width;
                    final cols = width >= 750 ? 2 : 1;
                    if (cols > 1) {
                      return GridView.builder(
                        shrinkWrap: true,
                        physics: const NeverScrollableScrollPhysics(),
                        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                          crossAxisCount: 2,
                          crossAxisSpacing: 10,
                          mainAxisSpacing: 10,
                          childAspectRatio: 3.8,
                        ),
                        itemCount: _filteredOthers.length,
                        itemBuilder: (context, index) => _buildContainerListItem(_filteredOthers[index]),
                      );
                    }

                    return Column(
                      children: _filteredOthers.map((c) => Padding(
                            padding: const EdgeInsets.only(bottom: 8),
                            child: _buildContainerListItem(c),
                          )).toList(),
                    );
                  }),
                ],
              ],
            ],
          ),
        ),
      ),
    ),
  ),
);
}

  Widget _buildAppListItem(ComposeApp app) {
    return DarkCard(
      onTap: () => _openApp(app),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      child: Row(
        children: [
          NivaroAppIcon(
            iconUrl: app.icon,
            name: app.title,
            imageName: app.image,
            size: 42,
            radius: 11,
            customRadiusPercent: app.iconRadius,
            isRunning: app.isRunning,
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Text(app.title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14, color: NivaroColors.textPrimary), maxLines: 1, overflow: TextOverflow.ellipsis),
                const SizedBox(height: 2),
                Text(
                  app.appType == 'system' ? 'System Application' : (app.isRunning ? 'Active and Running' : 'Stopped'),
                  style: TextStyle(
                    color: app.isRunning ? NivaroColors.successLight : NivaroColors.textMuted,
                    fontSize: 11.5,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
          if (app.appType != 'system')
            IconButton(
              icon: Icon(app.isRunning ? Icons.pause_rounded : Icons.play_arrow_rounded, size: 20),
              color: app.isRunning ? NivaroColors.warningLight : NivaroColors.successLight,
              onPressed: () => _toggleApp(app),
            ),
          const Icon(Icons.chevron_right_rounded, size: 18, color: NivaroColors.textFaint),
        ],
      ),
    );
  }

  Widget _buildContainerListItem(RawContainer c) {
    return DarkCard(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      child: Row(
        children: [
          NivaroAppIcon(
            iconUrl: c.icon,
            name: c.name,
            imageName: c.image,
            size: 38,
            radius: 10,
            isRunning: c.isRunning,
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Text(c.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5, color: NivaroColors.textPrimary), maxLines: 1, overflow: TextOverflow.ellipsis),
                const SizedBox(height: 2),
                Text(
                  '${c.image} · ${c.state.toUpperCase()}',
                  style: TextStyle(color: c.isRunning ? NivaroColors.successLight : NivaroColors.textMuted, fontSize: 11.5),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _FilterChip extends StatelessWidget {
  final String label;
  final bool selected;
  final VoidCallback onSelected;

  const _FilterChip({required this.label, required this.selected, required this.onSelected});

  @override
  Widget build(BuildContext context) {
    return InkWell(
      borderRadius: BorderRadius.circular(20),
      onTap: () {
        onSelected();
      },
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 13, vertical: 6),
        decoration: BoxDecoration(
          color: selected ? NivaroColors.primary.withOpacity(0.18) : NivaroColors.surfaceRaised,
          borderRadius: BorderRadius.circular(20),
          border: Border.all(
            color: selected ? NivaroColors.primaryLight.withOpacity(0.4) : NivaroColors.borderSubtle,
          ),
        ),
        child: Text(
          label,
          style: TextStyle(
            color: selected ? NivaroColors.primaryLight : NivaroColors.textMuted,
            fontSize: 12,
            fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
          ),
        ),
      ),
    );
  }
}
