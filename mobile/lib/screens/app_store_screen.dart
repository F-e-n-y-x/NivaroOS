import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/container_entry.dart';
import '../widgets/common.dart';
import 'custom_install_screen.dart';

/// Browse and install official apps from the catalog or paste custom compose files.
class AppStoreScreen extends StatefulWidget {
  const AppStoreScreen({super.key});

  @override
  State<AppStoreScreen> createState() => _AppStoreScreenState();
}

class _AppStoreScreenState extends State<AppStoreScreen> {
  List<StoreApp> _apps = [];
  Set<String> _installedIds = {};
  bool _loading = true;
  String? _error;
  final Set<String> _installing = {};
  final _searchController = TextEditingController();
  String _selectedCategory = 'All';

  final List<String> _categories = [
    'All',
    'Media',
    'Utilities',
    'Cloud & Sync',
    'Database',
    'Development',
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

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final res = await ApiClient.instance.get('/v2/app_management/apps');
      final data = res['data'] as Map<String, dynamic>? ?? {};
      final list = data['list'] as Map<String, dynamic>? ?? {};
      final installed = (data['installed'] as List<dynamic>? ?? []).map((e) => e.toString()).toSet();
      final apps = list.entries.map((e) => StoreApp.fromJson(e.key, e.value as Map<String, dynamic>)).toList();
      apps.sort((a, b) => a.title.toLowerCase().compareTo(b.title.toLowerCase()));
      if (!mounted) return;
      setState(() {
        _apps = apps;
        _installedIds = installed;
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

  Future<void> _install(StoreApp app) async {
    HapticFeedback.mediumImpact();
    setState(() => _installing.add(app.id));
    try {
      final composeRes = await ApiClient.instance.getWithAccept('/v2/app_management/apps/${app.id}/compose', 'application/yaml');
      if (composeRes.statusCode != 200) {
        throw Exception('Could not fetch install compose file (HTTP ${composeRes.statusCode}).');
      }
      await ApiClient.instance.postBody('/v2/app_management/compose', composeRes.body, 'application/yaml');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('${app.title} installed and deployed.'),
            backgroundColor: NivaroColors.success,
            behavior: SnackBarBehavior.floating,
          ),
        );
        Navigator.of(context).pop(true);
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(e.toString().replaceFirst('Exception: ', '')),
            backgroundColor: NivaroColors.danger,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    } finally {
      if (mounted) setState(() => _installing.remove(app.id));
    }
  }

  void _showAppDetails(StoreApp app) {
    HapticFeedback.lightImpact();
    final installed = _installedIds.contains(app.id);
    final installing = _installing.contains(app.id);

    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceRaised,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 20, 20, 24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  ClipRRect(
                    borderRadius: BorderRadius.circular(16),
                    child: app.icon.isNotEmpty
                        ? Image.network(app.icon, width: 56, height: 56, fit: BoxFit.cover, errorBuilder: (_, __, ___) => _fallback())
                        : _fallback(),
                  ),
                  const SizedBox(width: 16),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(app.title, style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 18)),
                        const SizedBox(height: 2),
                        Text(app.id, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13)),
                      ],
                    ),
                  ),
                  if (installed)
                    const StatusPill(label: 'Installed', state: 'running')
                ],
              ),
              const SizedBox(height: 16),
              if (app.tagline.isNotEmpty) ...[
                Text(
                  app.tagline,
                  style: const TextStyle(color: NivaroColors.textPrimary, fontSize: 14, height: 1.4),
                ),
                const SizedBox(height: 16),
              ],
              const Divider(height: 16),
              SizedBox(
                width: double.infinity,
                height: 48,
                child: installed
                    ? OutlinedButton.icon(
                        onPressed: () => Navigator.pop(context),
                        icon: const Icon(Icons.check_circle_outline_rounded, color: NivaroColors.success),
                        label: const Text('Already Installed'),
                      )
                    : FilledButton.icon(
                        onPressed: installing
                            ? null
                            : () {
                                Navigator.pop(context);
                                _install(app);
                              },
                        icon: installing
                            ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                            : const Icon(Icons.download_rounded, size: 20),
                        label: Text(installing ? 'Installing...' : 'Install App'),
                        style: FilledButton.styleFrom(
                          backgroundColor: NivaroColors.primary,
                          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                        ),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _customInstall() async {
    HapticFeedback.selectionClick();
    final installed = await Navigator.of(context).push<bool>(
      MaterialPageRoute(builder: (_) => const CustomInstallScreen()),
    );
    if (installed == true && mounted) Navigator.of(context).pop(true);
  }

  @override
  Widget build(BuildContext context) {
    final query = _searchController.text.trim().toLowerCase();
    var visible = query.isEmpty ? _apps : _apps.where((a) => a.title.toLowerCase().contains(query) || a.tagline.toLowerCase().contains(query)).toList();

    if (_selectedCategory != 'All') {
      visible = visible.where((a) {
        final cat = _inferCategory(a).toLowerCase();
        return cat.contains(_selectedCategory.toLowerCase());
      }).toList();
    }

    return Scaffold(
      body: SafeArea(
        child: Column(
          children: [
            // Top Bar
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 12, 20, 0),
              child: Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.arrow_back_rounded),
                    onPressed: () => Navigator.of(context).pop(),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'App Store',
                      style: Theme.of(context).textTheme.headlineMedium?.copyWith(
                            fontWeight: FontWeight.w800,
                            letterSpacing: -0.5,
                          ),
                    ),
                  ),
                  FilledButton.tonalIcon(
                    onPressed: _customInstall,
                    icon: const Icon(Icons.code_rounded, size: 18),
                    label: const Text('Custom'),
                    style: FilledButton.styleFrom(
                      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),

            // Search Box
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: TextField(
                controller: _searchController,
                decoration: InputDecoration(
                  hintText: 'Search catalog of server apps...',
                  prefixIcon: const Icon(Icons.search_rounded, size: 20),
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
                ),
              ),
            ),
            const SizedBox(height: 12),

            // Category Chips
            SizedBox(
              height: 38,
              child: ListView.separated(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                scrollDirection: Axis.horizontal,
                itemCount: _categories.length,
                separatorBuilder: (_, __) => const SizedBox(width: 8),
                itemBuilder: (context, i) {
                  final cat = _categories[i];
                  final isSelected = _selectedCategory == cat;
                  return FilterChip(
                    label: Text(cat),
                    selected: isSelected,
                    onSelected: (val) {
                      HapticFeedback.selectionClick();
                      setState(() => _selectedCategory = cat);
                    },
                    backgroundColor: NivaroColors.surfaceRaised,
                    selectedColor: NivaroColors.primary.withValues(alpha: 0.25),
                    labelStyle: TextStyle(
                      fontSize: 12,
                      fontWeight: isSelected ? FontWeight.w700 : FontWeight.w500,
                      color: isSelected ? NivaroColors.primaryLight : NivaroColors.textPrimary,
                    ),
                    shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(NivaroShape.full),
                      side: BorderSide(
                        color: isSelected ? NivaroColors.primary : NivaroColors.borderSubtle,
                      ),
                    ),
                  );
                },
              ),
            ),
            const SizedBox(height: 8),

            // App List
            Expanded(
              child: _loading
                  ? const Center(child: CircularProgressIndicator())
                  : _error != null
                      ? Center(
                          child: Padding(
                            padding: const EdgeInsets.all(40),
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                const Icon(Icons.error_outline_rounded, color: NivaroColors.danger, size: 36),
                                const SizedBox(height: 12),
                                Text(_error!, style: const TextStyle(color: NivaroColors.textMuted), textAlign: TextAlign.center),
                                const SizedBox(height: 16),
                                OutlinedButton(onPressed: _load, child: const Text('Retry')),
                              ],
                            ),
                          ),
                        )
                      : visible.isEmpty
                          ? const Center(
                              child: Padding(
                                padding: EdgeInsets.all(40),
                                child: Text('No applications match your search.', style: TextStyle(color: NivaroColors.textMuted)),
                              ),
                            )
                          : RefreshIndicator(
                              onRefresh: _load,
                              color: NivaroColors.primaryLight,
                              backgroundColor: NivaroColors.surfaceRaised,
                              child: ListView.separated(
                                padding: const EdgeInsets.fromLTRB(20, 8, 20, 40),
                                itemCount: visible.length,
                                separatorBuilder: (_, __) => const SizedBox(height: 10),
                                itemBuilder: (context, i) {
                                  final app = visible[i];
                                  final installed = _installedIds.contains(app.id);
                                  final installing = _installing.contains(app.id);

                                  return DarkCard(
                                    onTap: () => _showAppDetails(app),
                                    child: Row(
                                      children: [
                                        ClipRRect(
                                          borderRadius: BorderRadius.circular(NivaroShape.medium),
                                          child: app.icon.isNotEmpty
                                              ? Image.network(
                                                  app.icon,
                                                  width: 48,
                                                  height: 48,
                                                  fit: BoxFit.cover,
                                                  errorBuilder: (_, __, ___) => _fallback(),
                                                )
                                              : _fallback(),
                                        ),
                                        const SizedBox(width: 14),
                                        Expanded(
                                          child: Column(
                                            crossAxisAlignment: CrossAxisAlignment.start,
                                            children: [
                                              Text(
                                                app.title,
                                                style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15),
                                                maxLines: 1,
                                                overflow: TextOverflow.ellipsis,
                                              ),
                                              if (app.tagline.isNotEmpty) ...[
                                                const SizedBox(height: 2),
                                                Text(
                                                  app.tagline,
                                                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                                                  maxLines: 1,
                                                  overflow: TextOverflow.ellipsis,
                                                ),
                                              ],
                                            ],
                                          ),
                                        ),
                                        const SizedBox(width: 10),
                                        if (installed)
                                          Container(
                                            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
                                            decoration: BoxDecoration(
                                              color: NivaroColors.surfaceMuted,
                                              borderRadius: BorderRadius.circular(NivaroShape.full),
                                              border: Border.all(color: NivaroColors.borderSubtle),
                                            ),
                                            child: const Text('Installed', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11.5, fontWeight: FontWeight.w600)),
                                          )
                                        else
                                          FilledButton(
                                            onPressed: installing ? null : () => _install(app),
                                            style: FilledButton.styleFrom(
                                              backgroundColor: NivaroColors.primary,
                                              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
                                              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.medium)),
                                            ),
                                            child: installing
                                                ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                                                : const Text('Get', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13)),
                                          ),
                                      ],
                                    ),
                                  );
                                },
                              ),
                            ),
            ),
          ],
        ),
      ),
    );
  }

  String _inferCategory(StoreApp app) {
    final title = app.title.toLowerCase();
    final tag = app.tagline.toLowerCase();
    if (title.contains('plex') || title.contains('jellyfin') || title.contains('audio') || title.contains('media') || tag.contains('streaming') || tag.contains('video')) {
      return 'Media';
    }
    if (title.contains('nextcloud') || title.contains('syncthing') || title.contains('drive') || tag.contains('cloud') || tag.contains('sync')) {
      return 'Cloud & Sync';
    }
    if (title.contains('sql') || title.contains('mongo') || title.contains('redis') || title.contains('db') || tag.contains('database')) {
      return 'Database';
    }
    if (title.contains('code') || title.contains('git') || title.contains('dev') || tag.contains('developer') || tag.contains('editor')) {
      return 'Development';
    }
    return 'Utilities';
  }

  Widget _fallback() => Container(
        width: 48,
        height: 48,
        decoration: BoxDecoration(
          color: NivaroColors.surfaceMuted,
          borderRadius: BorderRadius.circular(NivaroShape.medium),
        ),
        alignment: Alignment.center,
        child: const Icon(Icons.widgets_rounded, color: NivaroColors.textMuted, size: 24),
      );
}

