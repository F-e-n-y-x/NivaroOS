import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/container_entry.dart';
import '../widgets/common.dart';
import '../utils/app_icons.dart';

class AppStoreScreen extends StatefulWidget {
  const AppStoreScreen({super.key});

  @override
  State<AppStoreScreen> createState() => _AppStoreScreenState();
}

class _AppStoreScreenState extends State<AppStoreScreen> {
  List<StoreApp> _catalog = [];
  bool _loading = true;
  String? _error;
  String _selectedCategory = 'all';
  final _searchController = TextEditingController();
  final Set<String> _installing = {};

  final _featuredApps = const [
    (id: 'plex', title: 'Plex Media Server', desc: 'Stream video, music and photos across all devices.', category: 'Media', icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/plex.png'),
    (id: 'nextcloud', title: 'Nextcloud Hub', desc: 'Secure self-hosted productivity platform and files.', category: 'Cloud', icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/nextcloud.png'),
    (id: 'homeassistant', title: 'Home Assistant', desc: 'Open-source local home automation system.', category: 'Smart Home', icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/home-assistant.png'),
    (id: 'jellyfin', title: 'Jellyfin Media', desc: 'Free software media system with zero tracking.', category: 'Media', icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/jellyfin.png'),
    (id: 'adguard-home', title: 'AdGuard Home', desc: 'Network-wide ads and trackers blocking DNS.', category: 'Network', icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/adguard-home.png'),
    (id: 'ollama', title: 'Ollama AI Server', desc: 'Run LLMs (Llama 3, DeepSeek, Mistral) locally.', category: 'AI & LLM', icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/ollama.png'),
  ];

  List<String> _categories = const ['all', 'Featured', 'Media', 'Cloud', 'Smart Home', 'Network', 'AI & LLM', 'Dev', 'Utilities'];

  static final List<StoreApp> _fallbackApps = [
    StoreApp(
      id: 'plex',
      title: 'Plex Media Server',
      tagline: 'Stream movies, TV shows, music, and photos anywhere, anytime.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/plex.png',
      category: 'Media',
      author: 'Plex, Inc.',
    ),
    StoreApp(
      id: 'jellyfin',
      title: 'Jellyfin',
      tagline: 'The Free Software Media System that puts you in control of managing and streaming your media.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/jellyfin.png',
      category: 'Media',
      author: 'Jellyfin',
    ),
    StoreApp(
      id: 'nextcloud',
      title: 'Nextcloud Hub',
      tagline: 'A safe home for all your data. Access & share your files, calendars, contacts, mail & more.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/nextcloud.png',
      category: 'Cloud',
      author: 'Nextcloud GmbH',
    ),
    StoreApp(
      id: 'homeassistant',
      title: 'Home Assistant',
      tagline: 'Open source home automation that puts local control and privacy first.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/home-assistant.png',
      category: 'Smart Home',
      author: 'Nabu Casa',
    ),
    StoreApp(
      id: 'adguard-home',
      title: 'AdGuard Home',
      tagline: 'Network-wide software for blocking ads & tracking across all home devices.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/adguard-home.png',
      category: 'Network',
      author: 'AdGuard',
    ),
    StoreApp(
      id: 'ollama',
      title: 'Ollama AI',
      tagline: 'Get up and running with Llama 3, Mistral, Gemma, DeepSeek and other large language models locally.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/ollama.png',
      category: 'AI & LLM',
      author: 'Ollama',
    ),
    StoreApp(
      id: 'qbittorrent',
      title: 'qBittorrent',
      tagline: 'The qBittorrent project aims to provide an open-source software alternative to µTorrent.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/qbittorrent.png',
      category: 'Utilities',
      author: 'qBittorrent',
    ),
    StoreApp(
      id: 'vaultwarden',
      title: 'Vaultwarden',
      tagline: 'Unofficial Bitwarden compatible server written in Rust.',
      icon: 'https://raw.githubusercontent.com/walkxcode/dashboard-icons/main/png/vaultwarden.png',
      category: 'Cloud',
      author: 'Vaultwarden',
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

  List<StoreApp> get _filteredApps {
    final query = _searchController.text.trim().toLowerCase();
    var list = _catalog;
    if (_selectedCategory != 'all' && _selectedCategory != 'Featured') {
      list = list.where((a) => a.category.toLowerCase().contains(_selectedCategory.toLowerCase())).toList();
    }
    if (query.isNotEmpty) {
      list = list.where((a) => a.title.toLowerCase().contains(query) || a.tagline.toLowerCase().contains(query) || a.id.toLowerCase().contains(query)).toList();
    }
    return list;
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final res = await ApiClient.instance.get('/v2/app_management/apps');
      dynamic data = res['data'] ?? res;
      final List<StoreApp> list = [];

      if (data is Map<String, dynamic>) {
        if (data['list'] is Map) {
          final map = data['list'] as Map;
          for (final entry in map.entries) {
            if (entry.value is Map) {
              list.add(StoreApp.fromJson(entry.key.toString(), Map<String, dynamic>.from(entry.value as Map)));
            }
          }
        } else if (data['list'] is List) {
          for (final item in data['list'] as List) {
            if (item is Map) {
              final id = item['id']?.toString() ?? (item['name']?.toString() ?? 'app');
              list.add(StoreApp.fromJson(id, Map<String, dynamic>.from(item)));
            }
          }
        } else if (data['apps'] is Map) {
          final map = data['apps'] as Map;
          for (final entry in map.entries) {
            if (entry.value is Map) {
              list.add(StoreApp.fromJson(entry.key.toString(), Map<String, dynamic>.from(entry.value as Map)));
            }
          }
        } else if (data['apps'] is List) {
          for (final item in data['apps'] as List) {
            if (item is Map) {
              final id = item['id']?.toString() ?? (item['name']?.toString() ?? 'app');
              list.add(StoreApp.fromJson(id, Map<String, dynamic>.from(item)));
            }
          }
        } else if (data['community'] is List || data['recommended'] is List) {
          final comList = (data['community'] as List? ?? []);
          final recList = (data['recommended'] as List? ?? []);
          for (final item in [...recList, ...comList]) {
            if (item is Map) {
              final id = item['id']?.toString() ?? (item['name']?.toString() ?? 'app');
              list.add(StoreApp.fromJson(id, Map<String, dynamic>.from(item)));
            }
          }
        } else {
          for (final entry in data.entries) {
            if (entry.value is Map && entry.key != 'installed') {
              list.add(StoreApp.fromJson(entry.key, Map<String, dynamic>.from(entry.value as Map)));
            }
          }
        }
      } else if (data is List) {
        for (final item in data) {
          if (item is Map) {
            final id = item['id']?.toString() ?? (item['name']?.toString() ?? 'app');
            list.add(StoreApp.fromJson(id, Map<String, dynamic>.from(item)));
          }
        }
      }

      // If remote was empty, keep fallback apps
      if (list.isEmpty) {
        list.addAll(_fallbackApps);
      }

      // Deduplicate by ID
      final seen = <String>{};
      final uniqueList = <StoreApp>[];
      for (final a in list) {
        if (!seen.contains(a.id.toLowerCase())) {
          seen.add(a.id.toLowerCase());
          uniqueList.add(a);
        }
      }
      uniqueList.sort((a, b) => a.title.toLowerCase().compareTo(b.title.toLowerCase()));

      // Dynamically extract categories from catalog
      final dynamicCategories = <String>{'all', 'Featured'};
      for (final a in uniqueList) {
        if (a.category.isNotEmpty) {
          dynamicCategories.add(a.category);
        }
      }

      if (!mounted) return;
      setState(() {
        _catalog = uniqueList;
        _categories = dynamicCategories.toList();
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

  Future<void> _installApp(String id, String title) async {
    HapticFeedback.mediumImpact();
    final portCtrl = TextEditingController();
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.largeIncreased)),
        title: Text('Install $title', style: const TextStyle(fontWeight: FontWeight.w700)),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Deploy container app "$id" to NivaroOS with automatic volumes and network mappings.', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13)),
            const SizedBox(height: 16),
            TextField(
              controller: portCtrl,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(
                labelText: 'Web Port (Optional Override)',
                hintText: 'e.g. 8080',
                filled: true,
                fillColor: NivaroColors.surfaceRaised,
              ),
            ),
          ],
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Install Now'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    setState(() => _installing.add(id));
    try {
      final body = <String, dynamic>{};
      if (portCtrl.text.trim().isNotEmpty) {
        body['port_map'] = portCtrl.text.trim();
      }
      await ApiClient.instance.post('/v2/app_management/compose/$id', body: body);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Installing $title in background...')),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Install failed: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _installing.remove(id));
    }
  }

  @override
  Widget build(BuildContext context) {
    final apps = _filteredApps;
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('Nivaro App Store', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 19)),
        backgroundColor: NivaroColors.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        actions: [
          RoundIconButton(
            icon: Icons.refresh_rounded,
            tooltip: 'Refresh Store',
            onPressed: _load,
          ),
          const SizedBox(width: 12),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        color: NivaroColors.primaryLight,
        backgroundColor: NivaroColors.surfaceRaised,
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 1150),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 14, 20, 100),
          children: [
            // Search Field
            TextField(
              controller: _searchController,
              decoration: InputDecoration(
                hintText: 'Search 150+ self-hosted applications...',
                hintStyle: const TextStyle(color: NivaroColors.textFaint, fontSize: 13),
                prefixIcon: const Icon(Icons.search_rounded, size: 20, color: NivaroColors.textFaint),
                filled: true,
                fillColor: NivaroColors.surface,
                contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                border: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(NivaroShape.large),
                  borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                ),
                enabledBorder: OutlineInputBorder(
                  borderRadius: BorderRadius.circular(NivaroShape.large),
                  borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                ),
              ),
            ),
            const SizedBox(height: 12),

            // Categories
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: _categories.map((cat) {
                  final sel = _selectedCategory == cat;
                  return Padding(
                    padding: const EdgeInsets.only(right: 8),
                    child: InkWell(
                      onTap: () => setState(() => _selectedCategory = cat),
                      borderRadius: BorderRadius.circular(20),
                      child: Container(
                        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 7),
                        decoration: BoxDecoration(
                          color: sel ? NivaroColors.primary : NivaroColors.surface,
                          borderRadius: BorderRadius.circular(20),
                          border: Border.all(color: sel ? Colors.transparent : NivaroColors.borderSubtle),
                        ),
                        child: Text(
                          cat == 'all' ? 'All' : cat,
                          style: TextStyle(
                            fontSize: 12.5,
                            fontWeight: sel ? FontWeight.w700 : FontWeight.w600,
                            color: sel ? Colors.white : NivaroColors.textMuted,
                          ),
                        ),
                      ),
                    ),
                  );
                }).toList(),
              ),
            ),
            const SizedBox(height: 18),

            // Featured Carousel
            if (_selectedCategory == 'all' || _selectedCategory == 'Featured') ...[
              const SectionHeader(title: 'Featured Apps'),
              SizedBox(
                height: 145,
                child: ListView.builder(
                  scrollDirection: Axis.horizontal,
                  itemCount: _featuredApps.length,
                  itemBuilder: (context, i) {
                    final f = _featuredApps[i];
                    final busy = _installing.contains(f.id);

                    return Container(
                      width: 270,
                      margin: const EdgeInsets.only(right: 12),
                      padding: const EdgeInsets.all(14),
                      decoration: BoxDecoration(
                        color: NivaroColors.surface,
                        borderRadius: BorderRadius.circular(NivaroShape.largeIncreased),
                        border: Border.all(color: NivaroColors.borderSubtle),
                        boxShadow: const [
                          BoxShadow(color: Color(0x33000000), blurRadius: 10, offset: Offset(0, 3)),
                        ],
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisAlignment: MainAxisAlignment.spaceBetween,
                        children: [
                          Row(
                            children: [
                              NivaroAppIcon(iconUrl: f.icon, name: f.title, size: 36, radius: 10),
                              const SizedBox(width: 10),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(f.title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5), overflow: TextOverflow.ellipsis),
                                    Text(f.category, style: const TextStyle(color: NivaroColors.primaryLight, fontSize: 11, fontWeight: FontWeight.w600)),
                                  ],
                                ),
                              ),
                            ],
                          ),
                          Text(
                            f.desc,
                            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          ),
                          Align(
                            alignment: Alignment.centerRight,
                            child: FilledButton(
                              onPressed: busy ? null : () => _installApp(f.id, f.title),
                              style: FilledButton.styleFrom(
                                backgroundColor: NivaroColors.primary,
                                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 4),
                                minimumSize: const Size(0, 28),
                                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
                              ),
                              child: busy
                                  ? const SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                                  : const Text('Install', style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w700)),
                            ),
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ),
              const SizedBox(height: 20),
            ],

            const SectionHeader(title: 'App Catalog'),
            if (_loading && _catalog.isEmpty)
              const Center(child: Padding(padding: EdgeInsets.all(40), child: CircularProgressIndicator()))
            else if (_error != null && _catalog.isEmpty)
              DarkCard(
                child: Center(
                  child: Column(
                    children: [
                      const Icon(Icons.error_outline_rounded, color: NivaroColors.dangerLight, size: 36),
                      const SizedBox(height: 10),
                      Text(_error!, style: const TextStyle(color: NivaroColors.textMuted)),
                      const SizedBox(height: 12),
                      OutlinedButton(onPressed: _load, child: const Text('Retry')),
                    ],
                  ),
                ),
              )
            else
              Builder(builder: (context) {
                final width = MediaQuery.of(context).size.width;
                final cols = width >= 1050 ? 3 : (width >= 700 ? 2 : 1);

                if (cols > 1) {
                  return GridView.builder(
                    shrinkWrap: true,
                    physics: const NeverScrollableScrollPhysics(),
                    gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                      crossAxisCount: cols,
                      crossAxisSpacing: 10,
                      mainAxisSpacing: 10,
                      childAspectRatio: 3.5,
                    ),
                    itemCount: apps.length,
                    itemBuilder: (context, index) {
                      final app = apps[index];
                      final busy = _installing.contains(app.id);
                      return DarkCard(
                        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                        child: Row(
                          children: [
                            NivaroAppIcon(
                              iconUrl: app.icon,
                              name: app.title,
                              size: 40,
                              radius: 11,
                            ),
                            const SizedBox(width: 12),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                mainAxisAlignment: MainAxisAlignment.center,
                                children: [
                                  Text(app.title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5), overflow: TextOverflow.ellipsis),
                                  const SizedBox(height: 2),
                                  Text(
                                    app.tagline.isNotEmpty ? app.tagline : app.category,
                                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ],
                              ),
                            ),
                            const SizedBox(width: 8),
                            FilledButton(
                              onPressed: busy ? null : () => _installApp(app.id, app.title),
                              style: FilledButton.styleFrom(
                                backgroundColor: NivaroColors.primary,
                                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
                                minimumSize: const Size(0, 30),
                                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                              ),
                              child: busy
                                  ? const SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                                  : const Text('Get', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 12)),
                            ),
                          ],
                        ),
                      );
                    },
                  );
                }

                return Column(
                  children: apps.map((app) {
                    final busy = _installing.contains(app.id);
                    return Padding(
                      padding: const EdgeInsets.only(bottom: 10),
                      child: DarkCard(
                        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
                        child: Row(
                          children: [
                            NivaroAppIcon(
                              iconUrl: app.icon,
                              name: app.title,
                              size: 42,
                              radius: 12,
                            ),
                            const SizedBox(width: 14),
                            Expanded(
                              child: Column(
                                crossAxisAlignment: CrossAxisAlignment.start,
                                children: [
                                  Text(app.title, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14), overflow: TextOverflow.ellipsis),
                                  const SizedBox(height: 2),
                                  Text(
                                    app.tagline.isNotEmpty ? app.tagline : app.category,
                                    style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                                    maxLines: 1,
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ],
                              ),
                            ),
                            const SizedBox(width: 8),
                            FilledButton(
                              onPressed: busy ? null : () => _installApp(app.id, app.title),
                              style: FilledButton.styleFrom(
                                backgroundColor: NivaroColors.primary,
                                padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
                                minimumSize: const Size(0, 32),
                                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                              ),
                              child: busy
                                  ? const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                                  : const Text('Get', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 12.5)),
                            ),
                          ],
                        ),
                      ),
                    );
                  }).toList(),
                );
              }),
            ],
            ),
          ),
        ),
      ),
    );
  }
}
