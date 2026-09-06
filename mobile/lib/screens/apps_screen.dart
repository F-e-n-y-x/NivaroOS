import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:url_launcher/url_launcher.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/container_entry.dart';
import '../widgets/common.dart';
import 'app_store_screen.dart';

/// Installed applications & Docker container management with Grid/List switcher,
/// real-time status control, and web launcher.
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
  final Set<String> _busy = {};
  final _searchController = TextEditingController();

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
    if (query.isEmpty) return _apps;
    return _apps.where((a) => a.title.toLowerCase().contains(query) || a.id.toLowerCase().contains(query)).toList();
  }

  List<RawContainer> get _filteredOthers {
    final query = _searchController.text.trim().toLowerCase();
    if (query.isEmpty) return _others;
    return _others.where((c) => c.name.toLowerCase().contains(query)).toList();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final composeRes = await ApiClient.instance.get('/v2/app_management/compose');
      final composeData = composeRes['data'] as Map<String, dynamic>? ?? {};
      final apps = composeData.entries.map((e) => ComposeApp.fromJson(e.key, e.value as Map<String, dynamic>)).toList();
      apps.sort((a, b) => a.title.toLowerCase().compareTo(b.title.toLowerCase()));

      var others = <RawContainer>[];
      try {
        final containerRes = await ApiClient.instance.get('/container/all');
        final containerData = containerRes['data'] as List<dynamic>? ?? [];
        final containers = containerData.map((e) => RawContainer.fromJson(e as Map<String, dynamic>)).toList();
        final composeIds = apps.map((a) => a.id.toLowerCase()).toList();
        others = containers.where((c) {
          final name = c.name.toLowerCase();
          return !composeIds.any((id) => name == id || name.startsWith('${id}_') || name.startsWith('$id-'));
        }).toList();
        others.sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
      } catch (_) {}

      if (!mounted) return;
      setState(() {
        _apps = apps;
        _others = others;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e.toString().replaceFirst('Exception: ', '');
        _loading = false;
      });
    }
  }

  Future<void> _setStatus(ComposeApp app, String action) async {
    HapticFeedback.mediumImpact();
    setState(() => _busy.add(app.id));
    try {
      await ApiClient.instance.put('/v2/app_management/compose/${app.id}/status', body: action);
      await Future.delayed(const Duration(milliseconds: 500));
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busy.remove(app.id));
    }
  }

  Future<void> _uninstall(ComposeApp app) async {
    HapticFeedback.mediumImpact();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Uninstall Application'),
        content: Text('Uninstall "${app.title}"? All associated containers will be removed.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          TextButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Uninstall', style: TextStyle(color: NivaroColors.danger, fontWeight: FontWeight.bold)),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    setState(() => _busy.add(app.id));
    try {
      await ApiClient.instance.delete('/v2/app_management/compose/${app.id}', query: {'delete_config_folder': false});
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busy.remove(app.id));
    }
  }

  Future<void> _setRawState(RawContainer c, String state) async {
    HapticFeedback.mediumImpact();
    setState(() => _busy.add(c.id));
    try {
      await ApiClient.instance.put('/container/${c.id}/state', body: {'state': state});
      await Future.delayed(const Duration(milliseconds: 500));
      await _load();
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busy.remove(c.id));
    }
  }

  Future<void> _openInBrowser(ComposeApp app) async {
    HapticFeedback.lightImpact();
    setState(() => _busy.add(app.id));
    try {
      final res = await ApiClient.instance.get('/v2/app_management/compose/${app.id}/containers');
      final data = res['data'] as Map<String, dynamic>? ?? {};
      final mainService = data['main'] as String?;
      final containers = data['containers'] as Map<String, dynamic>? ?? {};

      Map<String, dynamic>? mainContainer;
      for (final c in containers.values) {
        final container = c as Map<String, dynamic>;
        if (mainService == null || container['Service'] == mainService) {
          mainContainer = container;
          break;
        }
      }
      final publishers = (mainContainer?['Publishers'] as List<dynamic>?) ?? [];
      if (publishers.isEmpty) {
        throw Exception('This app has no published web interface port.');
      }
      final port = (publishers.first as Map<String, dynamic>)['PublishedPort'];
      final host = Uri.parse(ApiClient.instance.baseUrl).host;
      final url = Uri.parse('http://$host:$port');
      final launched = await launchUrl(url, mode: LaunchMode.externalApplication);
      if (!launched) throw Exception('Could not launch browser.');
    } catch (e) {
      if (mounted) _showError(e);
    } finally {
      if (mounted) setState(() => _busy.remove(app.id));
    }
  }

  void _showError(Object e) {
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(e.toString().replaceFirst('Exception: ', '')),
        backgroundColor: NivaroColors.danger,
      ),
    );
  }

  void _showMenu(ComposeApp app) {
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceContainerHigh,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(width: 36, height: 4, decoration: BoxDecoration(color: NivaroColors.borderHighlight, borderRadius: BorderRadius.circular(2))),
              const SizedBox(height: 12),
              ListTile(
                leading: ClipRRect(
                  borderRadius: BorderRadius.circular(NivaroShape.medium),
                  child: app.icon.isNotEmpty
                      ? Image.network(app.icon, width: 44, height: 44, fit: BoxFit.cover, errorBuilder: (_, __, ___) => _fallbackIcon(app.title))
                      : _fallbackIcon(app.title),
                ),
                title: Text(app.title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
                subtitle: Text(app.id, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                trailing: StatusPill(label: app.isRunning ? 'Running' : 'Stopped', state: app.status),
              ),
              const Divider(height: 16),
              ListTile(
                leading: const Icon(Icons.open_in_browser_rounded, color: NivaroColors.primaryLight),
                title: const Text('Open Web Interface', style: TextStyle(fontWeight: FontWeight.w600)),
                onTap: () {
                  Navigator.pop(context);
                  _openInBrowser(app);
                },
              ),
              if (app.isRunning) ...[
                ListTile(
                  leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.info),
                  title: const Text('Restart Container'),
                  onTap: () {
                    Navigator.pop(context);
                    _setStatus(app, 'restart');
                  },
                ),
                ListTile(
                  leading: const Icon(Icons.stop_circle_outlined, color: NivaroColors.warning),
                  title: const Text('Stop Container'),
                  onTap: () {
                    Navigator.pop(context);
                    _setStatus(app, 'stop');
                  },
                ),
              ] else
                ListTile(
                  leading: const Icon(Icons.play_circle_outline, color: NivaroColors.success),
                  title: const Text('Start Container'),
                  onTap: () {
                    Navigator.pop(context);
                    _setStatus(app, 'start');
                  },
                ),
              ListTile(
                leading: const Icon(Icons.delete_outline, color: NivaroColors.danger),
                title: const Text('Uninstall App', style: TextStyle(color: NivaroColors.danger)),
                onTap: () {
                  Navigator.pop(context);
                  _uninstall(app);
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _showRawContainerMenu(RawContainer c) {
    HapticFeedback.lightImpact();
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceRaised,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              ListTile(
                leading: _fallbackIcon(c.name),
                title: Text(c.name, style: const TextStyle(fontWeight: FontWeight.w700)),
                subtitle: Text(c.image, maxLines: 1, overflow: TextOverflow.ellipsis),
                trailing: StatusPill(label: c.isRunning ? 'Running' : 'Stopped', state: c.state),
              ),
              const Divider(height: 24),
              if (c.isRunning) ...[
                ListTile(
                  leading: const Icon(Icons.stop_circle_outlined, color: NivaroColors.warning),
                  title: const Text('Stop Container'),
                  onTap: () {
                    Navigator.pop(context);
                    _setRawState(c, 'stop');
                  },
                ),
                ListTile(
                  leading: const Icon(Icons.restart_alt_rounded, color: NivaroColors.primaryLight),
                  title: const Text('Restart Container'),
                  onTap: () {
                    Navigator.pop(context);
                    _setRawState(c, 'restart');
                  },
                ),
              ] else
                ListTile(
                  leading: const Icon(Icons.play_circle_outline, color: NivaroColors.success),
                  title: const Text('Start Container'),
                  onTap: () {
                    Navigator.pop(context);
                    _setRawState(c, 'start');
                  },
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _fallbackIcon(String name) {
    final color = monogramColorFor(name);
    return Container(
      width: 44,
      height: 44,
      decoration: BoxDecoration(color: color.withOpacity(0.16), borderRadius: BorderRadius.circular(12)),
      alignment: Alignment.center,
      child: Text(name.isNotEmpty ? name[0].toUpperCase() : '?', style: TextStyle(color: color, fontWeight: FontWeight.bold, fontSize: 18)),
    );
  }

  Future<void> _openStore() async {
    final installed = await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => const AppStoreScreen()),
    );
    if (installed == true) _load();
  }

  @override
  Widget build(BuildContext context) {
    final apps = _filteredApps;
    final others = _filteredOthers;
    final totalCount = _apps.length + _others.length;

    return SafeArea(
      bottom: false,
      child: RefreshIndicator(
        onRefresh: _load,
        color: NivaroColors.primaryLight,
        backgroundColor: NivaroColors.surfaceRaised,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 14, 20, 140),
          children: [
            // Header Row
            Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'Applications',
                        style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                              fontWeight: FontWeight.w800,
                              letterSpacing: -0.5,
                            ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        '$totalCount total containers running & installed',
                        style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                      ),
                    ],
                  ),
                ),
                // View switcher & store button
                Container(
                  decoration: BoxDecoration(
                    color: NivaroColors.surfaceRaised,
                    borderRadius: BorderRadius.circular(NivaroShape.medium),
                    border: Border.all(color: NivaroColors.borderSubtle),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      IconButton(
                        icon: Icon(Icons.grid_view_rounded, size: 20, color: _isGridView ? NivaroColors.primaryLight : NivaroColors.textMuted),
                        onPressed: () {
                          HapticFeedback.selectionClick();
                          setState(() => _isGridView = true);
                        },
                        tooltip: 'Grid view',
                      ),
                      IconButton(
                        icon: Icon(Icons.view_list_rounded, size: 20, color: !_isGridView ? NivaroColors.primaryLight : NivaroColors.textMuted),
                        onPressed: () {
                          HapticFeedback.selectionClick();
                          setState(() => _isGridView = false);
                        },
                        tooltip: 'List view',
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                FilledButton.icon(
                  onPressed: _openStore,
                  icon: const Icon(Icons.add_rounded, size: 18),
                  label: const Text('Store'),
                  style: FilledButton.styleFrom(
                    backgroundColor: NivaroColors.primary,
                    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),

            // Search Bar
            TextField(
              controller: _searchController,
              onChanged: (_) => setState(() {}),
              decoration: InputDecoration(
                hintText: 'Search applications & containers...',
                prefixIcon: const Icon(Icons.search_rounded, size: 20, color: NivaroColors.textMuted),
                suffixIcon: _searchController.text.isNotEmpty
                    ? IconButton(
                        icon: const Icon(Icons.clear_rounded, size: 18),
                        onPressed: () {
                          _searchController.clear();
                          setState(() {});
                        },
                      )
                    : null,
                filled: true,
                fillColor: NivaroColors.surfaceRaised,
                contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(NivaroShape.large),
                  borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                ),
                enabledBorder: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(NivaroShape.large),
                  borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                ),
                focusedBorder: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(NivaroShape.large),
                  borderSide: const BorderSide(color: NivaroColors.primary, width: 1.5),
                ),
              ),
            ),
            const SizedBox(height: 20),

            if (_loading && _apps.isEmpty)
              const Center(
                child: Padding(
                  padding: EdgeInsets.all(40),
                  child: CircularProgressIndicator(),
                ),
              )
            else if (_error != null && _apps.isEmpty)
              DarkCard(
                child: Center(
                  child: Column(
                    children: [
                      const Icon(Icons.error_outline_rounded, color: NivaroColors.danger, size: 36),
                      const SizedBox(height: 10),
                      Text(_error!, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                      const SizedBox(height: 12),
                      OutlinedButton(onPressed: _load, child: const Text('Retry')),
                    ],
                  ),
                ),
              )
            else if (apps.isEmpty && others.isEmpty)
              DarkCard(
                padding: const EdgeInsets.all(36),
                child: Center(
                  child: Column(
                    children: [
                      const Icon(Icons.apps_rounded, color: NivaroColors.textMuted, size: 48),
                      const SizedBox(height: 14),
                      const Text('No apps installed yet', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 16)),
                      const SizedBox(height: 6),
                      const Text('Discover and deploy container apps in one tap.', style: TextStyle(color: NivaroColors.textMuted, fontSize: 13), textAlign: TextAlign.center),
                      const SizedBox(height: 16),
                      ElevatedButton.icon(
                        onPressed: _openStore,
                        icon: const Icon(Icons.storefront_rounded, size: 18),
                        label: const Text('Browse App Store'),
                      ),
                    ],
                  ),
                ),
              )
            else if (_isGridView) ...[
              // Grid View
              GridView.count(
                crossAxisCount: 4,
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                mainAxisSpacing: 18,
                crossAxisSpacing: 14,
                childAspectRatio: 0.74,
                children: [
                  ...apps.map((app) {
                    final busy = _busy.contains(app.id);
                    return AppIconTile(
                      name: app.title,
                      iconUrl: app.icon,
                      running: app.isRunning,
                      onTap: busy ? () {} : () => _showMenu(app),
                      badge: busy
                          ? const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2))
                          : app.updateAvailable
                              ? Container(width: 12, height: 12, decoration: const BoxDecoration(color: NivaroColors.warning, shape: BoxShape.circle))
                              : null,
                    );
                  }),
                  ...others.map((c) {
                    final busy = _busy.contains(c.id);
                    return AppIconTile(
                      name: c.name,
                      iconUrl: '',
                      running: c.isRunning,
                      onTap: busy ? () {} : () => _showRawContainerMenu(c),
                      badge: busy ? const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2)) : null,
                    );
                  }),
                ],
              ),
            ] else ...[
              // List View
              ...apps.map((app) {
                final busy = _busy.contains(app.id);
                return Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: DarkCard(
                    onTap: () => _showMenu(app),
                    child: Row(
                      children: [
                        ClipRRect(
                          borderRadius: BorderRadius.circular(NivaroShape.medium),
                          child: app.icon.isNotEmpty
                              ? Image.network(app.icon, width: 44, height: 44, fit: BoxFit.cover, errorBuilder: (_, __, ___) => _fallbackIcon(app.title))
                              : _fallbackIcon(app.title),
                        ),
                        const SizedBox(width: 14),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(app.title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15), overflow: TextOverflow.ellipsis),
                              const SizedBox(height: 2),
                              Text(app.id, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12), overflow: TextOverflow.ellipsis),
                            ],
                          ),
                        ),
                        StatusPill(label: app.isRunning ? 'Running' : 'Stopped', state: app.status),
                        const SizedBox(width: 8),
                        if (busy)
                          const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                        else
                          IconButton(
                            icon: Icon(
                              app.isRunning ? Icons.open_in_browser_rounded : Icons.play_arrow_rounded,
                              color: app.isRunning ? NivaroColors.primaryLight : NivaroColors.success,
                            ),
                            tooltip: app.isRunning ? 'Open' : 'Start',
                            onPressed: () => app.isRunning ? _openInBrowser(app) : _setStatus(app, 'start'),
                          ),
                      ],
                    ),
                  ),
                );
              }),
              if (others.isNotEmpty) ...[
                const SizedBox(height: 16),
                const SectionHeader(title: 'Other Docker Containers'),
                const SizedBox(height: 8),
                ...others.map((c) {
                  final busy = _busy.contains(c.id);
                  return Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: DarkCard(
                      onTap: () => _showRawContainerMenu(c),
                      child: Row(
                        children: [
                          _fallbackIcon(c.name),
                          const SizedBox(width: 14),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(c.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15), overflow: TextOverflow.ellipsis),
                                const SizedBox(height: 2),
                                Text(c.image, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12), overflow: TextOverflow.ellipsis),
                              ],
                            ),
                          ),
                          StatusPill(label: c.isRunning ? 'Running' : 'Stopped', state: c.state),
                          const SizedBox(width: 8),
                          if (busy)
                            const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                          else
                            IconButton(
                              icon: Icon(
                                c.isRunning ? Icons.stop_rounded : Icons.play_arrow_rounded,
                                color: c.isRunning ? NivaroColors.warning : NivaroColors.success,
                              ),
                              tooltip: c.isRunning ? 'Stop' : 'Start',
                              onPressed: () => _setRawState(c, c.isRunning ? 'stop' : 'start'),
                            ),
                        ],
                      ),
                    ),
                  );
                }),
              ],
            ],
          ],
        ),
      ),
    );
  }
}
