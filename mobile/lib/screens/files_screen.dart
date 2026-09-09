import 'dart:io';
import 'dart:math';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../services/device_sync_service.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../services/shortcuts_service.dart';
import '../services/permission_service.dart';
import '../services/storage_service.dart';
import '../models/file_entry.dart';
import '../models/dashboard_stats.dart';
import '../models/cloud_account.dart';
import '../models/favorite_folder.dart';
import '../utils/format.dart';
import 'file_viewer_screen.dart';
import 'login_screen.dart';

enum FileCategoryFilter {
  all,
  folders,
  images,
  videos,
  documents,
  archives,
  code,
}

class FileTab {
  String id;
  String name;
  String path;
  String? locationRoot;
  bool atHome;
  bool isLocalDevice;
  List<String> history;
  int historyIndex;
  List<FileEntry> entries;
  final Set<String> selectedPaths = {};
  bool isSelectionMode = false;
  String searchQuery;
  bool isSearching;
  String viewMode; // 'compact' (default thumbnail grid), 'grid', 'list'
  String sortBy; // 'name', 'date', 'size', 'type'
  bool sortAscending;
  bool showHidden;
  FileCategoryFilter categoryFilter;

  FileTab({
    required this.id,
    required this.name,
    required this.path,
    this.locationRoot,
    this.atHome = true,
    this.isLocalDevice = false,
    List<String>? history,
    this.historyIndex = 0,
    List<FileEntry>? entries,
    this.searchQuery = '',
    this.isSearching = false,
    this.viewMode = 'compact',
    this.sortBy = 'name',
    this.sortAscending = true,
    this.showHidden = false,
    this.categoryFilter = FileCategoryFilter.all,
  })  : history = history ?? (path.isNotEmpty ? [path] : ['/DATA']),
        entries = entries ?? [];

  static FileTab initial() {
    return FileTab(
      id: 'tab_${DateTime.now().millisecondsSinceEpoch}',
      name: 'Storage',
      path: '/DATA',
      locationRoot: null,
      atHome: true,
      isLocalDevice: false,
      viewMode: 'compact',
    );
  }
}

class FilesScreen extends StatefulWidget {
  const FilesScreen({super.key});

  @override
  State<FilesScreen> createState() => FilesScreenState();
}

class FilesScreenState extends State<FilesScreen> {
  static const _defaultHomePath = '/DATA';
  static const _defaultLocalDevicePath = '/storage/emulated/0';

  final List<FileTab> _tabs = [FileTab.initial()];
  int _activeTabIndex = 0;
  bool _showTabsStrip = false;

  FileTab get _currentTab => _tabs[_activeTabIndex];

  final List<String> _clipboardPaths = [];
  String _clipboardOp = 'copy'; // 'copy' or 'move'
  bool _clipboardIsLocal = false;

  List<FavoriteFolder> _favorites = [];
  bool _favoritesLoading = true;

  List<DiskUsage> _disks = [];
  bool _disksLoading = true;

  List<CloudAccount> _cloudAccounts = [];
  List<CompanionDevice> _companionDevices = [];
  bool _companionLoading = false;

  bool _cloudLoading = true;

  final Map<String, List<FileEntry>> _folderCache = {};
  bool _loading = false;
  String? _error;
  final _searchController = TextEditingController();
  bool _isCurrentPathFavorited = false;

  // Live Copy / Paste Progress state
  bool _isTransferring = false;
  String _transferTitle = '';
  String _transferCurrentFile = '';
  int _transferCurrentIndex = 0;
  int _transferTotalCount = 0;
  double _transferProgress = 0.0;
  bool _transferCancelled = false;

  bool handleBack() {
    final tab = _currentTab;
    if (tab.isSelectionMode) {
      setState(() {
        tab.isSelectionMode = false;
        tab.selectedPaths.clear();
      });
      return true;
    }
    if (tab.isSearching) {
      setState(() {
        tab.isSearching = false;
        tab.searchQuery = '';
        _searchController.clear();
      });
      return true;
    }
    if (!tab.atHome) {
      _navigateUp();
      return true;
    }
    if (_tabs.length > 1) {
      _closeTab(_activeTabIndex);
      return true;
    }
    return false;
  }

  List<FileEntry> get _filteredEntries {
    final tab = _currentTab;
    final query = tab.searchQuery.trim().toLowerCase();
    var list = tab.entries;

    if (!tab.showHidden) {
      list = list.where((e) => !e.name.startsWith('.')).toList();
    }

    if (tab.categoryFilter != FileCategoryFilter.all) {
      list = list.where((e) {
        switch (tab.categoryFilter) {
          case FileCategoryFilter.folders:
            return e.isDir;
          case FileCategoryFilter.images:
            return !e.isDir && e.isImage;
          case FileCategoryFilter.videos:
            return !e.isDir && e.isVideo;
          case FileCategoryFilter.documents:
            return !e.isDir && (e.isPdf || e.isText || e.extension == 'doc' || e.extension == 'docx' || e.extension == 'xls' || e.extension == 'xlsx' || e.extension == 'ppt' || e.extension == 'pptx' || e.extension == 'md');
          case FileCategoryFilter.archives:
            return !e.isDir && (e.extension == 'zip' || e.extension == 'tar' || e.extension == 'gz' || e.extension == 'tgz' || e.extension == 'bz2' || e.extension == '7z' || e.extension == 'rar' || e.extension == 'iso');
          case FileCategoryFilter.code:
            return !e.isDir && (e.extension == 'json' || e.extension == 'yaml' || e.extension == 'yml' || e.extension == 'sh' || e.extension == 'py' || e.extension == 'js' || e.extension == 'ts' || e.extension == 'go' || e.extension == 'dart' || e.extension == 'html' || e.extension == 'css' || e.extension == 'conf');
          case FileCategoryFilter.all:
            return true;
        }
      }).toList();
    }

    if (query.isNotEmpty) {
      list = list.where((e) => e.name.toLowerCase().contains(query)).toList();
    }

    final sorted = List<FileEntry>.from(list);
    sorted.sort((a, b) {
      if (a.isDir != b.isDir) return a.isDir ? -1 : 1;

      int cmp = 0;
      switch (tab.sortBy) {
        case 'size':
          cmp = a.size.compareTo(b.size);
          break;
        case 'type':
          cmp = a.extension.compareTo(b.extension);
          break;
        case 'date':
          cmp = (a.modified ?? DateTime(1970)).compareTo(b.modified ?? DateTime(1970));
          break;
        case 'name':
        default:
          cmp = a.name.toLowerCase().compareTo(b.name.toLowerCase());
          break;
      }
      return tab.sortAscending ? cmp : -cmp;
    });

    return sorted;
  }

  @override
  void initState() {
    super.initState();
    _loadFavorites();
    _loadDisks();
    _loadCloudAccounts();
    _loadCompanionDevices();
    _searchController.addListener(() {
      setState(() {
        _currentTab.searchQuery = _searchController.text;
      });
    });
  }

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  void _addNewTab([String path = _defaultHomePath, String? name, bool isLocal = false]) {
    final tabName = name ?? (path == _defaultHomePath ? 'Storage' : path.split('/').where((s) => s.isNotEmpty).lastOrNull ?? 'Folder');
    final newTab = FileTab(
      id: 'tab_${DateTime.now().millisecondsSinceEpoch}',
      name: tabName,
      path: path,
      atHome: path == _defaultHomePath && !isLocal,
      isLocalDevice: isLocal,
      viewMode: 'compact',
    );
    setState(() {
      _tabs.add(newTab);
      _activeTabIndex = _tabs.length - 1;
      _showTabsStrip = true;
      _searchController.text = newTab.searchQuery;
    });
    if (!newTab.atHome) {
      _load();
    }
  }

  void _closeTab(int index) {
    if (_tabs.length <= 1) {
      setState(() {
        _tabs[0] = FileTab.initial();
        _activeTabIndex = 0;
        _showTabsStrip = false;
        _searchController.clear();
      });
      return;
    }
    setState(() {
      _tabs.removeAt(index);
      if (_activeTabIndex >= _tabs.length) {
        _activeTabIndex = _tabs.length - 1;
      }
      if (_tabs.length <= 1) {
        _showTabsStrip = false;
      }
      _searchController.text = _currentTab.searchQuery;
    });
  }

  void _switchTab(int index) {
    if (index == _activeTabIndex) return;
    setState(() {
      _activeTabIndex = index;
      _searchController.text = _currentTab.searchQuery;
    });
    if (!_currentTab.atHome && _currentTab.entries.isEmpty) {
      _load();
    }
  }

  Future<void> _loadFavorites() async {
    setState(() => _favoritesLoading = true);
    try {
      final favs = await ShortcutsService.instance.getAllFavorites();
      if (!mounted) return;
      setState(() {
        _favorites = favs;
        _favoritesLoading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() {
        _favorites = FavoriteFolder.defaultWebUiFavorites;
        _favoritesLoading = false;
      });
    }
  }

  Future<void> _loadDisks() async {
    setState(() => _disksLoading = true);
    try {
      final List<DiskUsage> allDisks = [];
      try {
        final res = await ApiClient.instance.get('/sys/disks-usage');
        final data = res['data'] as List<dynamic>? ?? [];
        for (final item in data) {
          if (item is Map<String, dynamic>) {
            final du = DiskUsage.fromJson(item);
            allDisks.add(du);
          }
        }
      } catch (e) {
        debugPrint('[FilesScreen] Error loading sys disks-usage: $e');
      }

      if (allDisks.isEmpty) {
        try {
          final res = await ApiClient.instance.get('/storage');
          final data = res['data'] ?? res;
          final list = DiskUsage.fromStorageApi(data);
          allDisks.addAll(list);
        } catch (_) {}
      }

      try {
        final usbRes = await ApiClient.instance.get('/disks/usb');
        final usbData = usbRes['data'] ?? usbRes;
        final usbDisks = DiskUsage.fromStorageApi(usbData);
        for (final ud in usbDisks) {
          if (!allDisks.any((d) => d.mountPoint == ud.mountPoint)) {
            allDisks.add(ud);
          }
        }
      } catch (_) {}

      final seen = <String>{};
      final uniqueDisks = <DiskUsage>[];
      for (final d in allDisks) {
        final mp = d.mountPoint.toLowerCase();
        // Skip internal VM pass-through mounts and phone local storage (shown in companion section)
        if (mp.startsWith('/data/vm-shares') || mp == '/storage/emulated/0' || mp.startsWith('/storage/emulated')) {
          continue;
        }
        if (!seen.contains(d.mountPoint) && d.mountPoint.isNotEmpty && !d.isSystemPartition) {
          seen.add(d.mountPoint);
          uniqueDisks.add(d);
        }
      }

      if (!mounted) return;
      setState(() {
        _disks = uniqueDisks;
        _disksLoading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _disksLoading = false);
    }
  }


  Future<void> _loadCompanionDevices() async {
    setState(() => _companionLoading = true);
    try {
      final list = await DeviceSyncService.instance.listCompanionDevices();
      if (!mounted) return;
      setState(() {
        _companionDevices = list;
        _companionLoading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _companionLoading = false);
    }
  }

  String _formatBytes(int bytes) {
    if (bytes <= 0) return '0 B';
    const suffixes = ['B', 'KB', 'MB', 'GB', 'TB'];
    final i = (log(bytes) / log(1024)).floor().clamp(0, suffixes.length - 1);
    final size = bytes / pow(1024, i);
    return '${size.toStringAsFixed(size >= 10 || i == 0 ? 0 : 1)} ${suffixes[i]}';
  }

  Future<void> _loadCloudAccounts() async {
    setState(() => _cloudLoading = true);
    try {
      final res = await ApiClient.instance.get('/cloud');
      final data = res['data'] as List<dynamic>? ?? [];
      final accounts = data.map((e) => CloudAccount.fromJson(e as Map<String, dynamic>)).toList();
      if (!mounted) return;
      setState(() {
        _cloudAccounts = accounts;
        _cloudLoading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _cloudLoading = false);
    }
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

  Future<void> _load({bool silent = false}) async {
    final tab = _currentTab;
    if (!silent) {
      setState(() {
        _loading = true;
        _error = null;
        tab.selectedPaths.clear();
        tab.isSelectionMode = false;
      });
    }

    if (tab.isLocalDevice) {
      try {
        final dir = Directory(tab.path);
        if (!dir.existsSync()) {
          throw Exception('Local directory not found.');
        }
        final entities = dir.listSync(followLinks: false);
        final entries = <FileEntry>[];
        for (final entity in entities) {
          try {
            final stat = entity.statSync();
            final name = entity.path.split(Platform.pathSeparator).where((s) => s.isNotEmpty).lastOrNull ?? entity.path;
            final isDir = entity is Directory;
            entries.add(FileEntry(
              name: name,
              path: entity.path,
              isDir: isDir,
              size: isDir ? 0 : stat.size,
              modified: stat.modified,
            ));
          } catch (_) {}
        }
        _folderCache[tab.path] = entries;
        if (!mounted) return;
        setState(() {
          tab.entries = entries;
          _isCurrentPathFavorited = false;
          _loading = false;
        });
      } catch (e) {
        if (!mounted) return;
        setState(() {
          _error = 'Local storage access error: $e';
          _loading = false;
        });
      }
      return;
    }

    try {
      final res = await ApiClient.instance.get('/folder', query: {'path': tab.path});
      final data = res['data'] as Map<String, dynamic>? ?? {};
      final content = (data['content'] as List<dynamic>? ?? []);
      final entries = content.map((e) => FileEntry.fromJson(e as Map<String, dynamic>)).toList();
      _folderCache[tab.path] = entries;
      final isFav = await ShortcutsService.instance.isFavorited(tab.path);
      if (!mounted) return;
      setState(() {
        tab.entries = entries;
        _isCurrentPathFavorited = isFav;
        _loading = false;
      });
    } catch (e) {
      if (!mounted) return;
      if (!silent || tab.entries.isEmpty) {
        setState(() {
          _error = e.toString().replaceFirst('Exception: ', '');
          _loading = false;
        });
      }
    }
  }

  Future<void> _openLocalDeviceStorage() async {
    final granted = await PermissionService.requestManageStorage();
    if (!granted && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Storage permission is required to browse phone files.'), backgroundColor: NivaroColors.warning),
      );
    }
    _openPath(_defaultLocalDevicePath, isLocal: true);
  }

  void _goToHome() {
    final tab = _currentTab;
    setState(() {
      tab.atHome = true;
      tab.isLocalDevice = false;
      tab.locationRoot = null;
      tab.path = _defaultHomePath;
      tab.name = 'Storage';
      tab.history = [_defaultHomePath];
      tab.historyIndex = 0;
      tab.isSearching = false;
      tab.searchQuery = '';
      tab.isSelectionMode = false;
      tab.selectedPaths.clear();
      _searchController.clear();
    });
    _loadFavorites();
    _loadDisks();
    _loadCloudAccounts();
    _loadCompanionDevices();
  }

  void _openPath(String target, {bool newTab = false, bool isLocal = false, bool fromHome = false}) {
    if (newTab) {
      _addNewTab(target, null, isLocal);
      return;
    }

    final tab = _currentTab;
    final cached = _folderCache[target];
    final wasAtHome = tab.atHome || fromHome;

    setState(() {
      tab.path = target;
      tab.name = isLocal ? 'Phone (${target.split("/").lastOrNull ?? "Storage"})' : (target.split('/').where((s) => s.isNotEmpty).lastOrNull ?? 'Folder');
      tab.atHome = false;
      tab.isLocalDevice = isLocal;
      if (wasAtHome) {
        tab.locationRoot = target;
        tab.history = [target];
        tab.historyIndex = 0;
      } else {
        if (tab.historyIndex < tab.history.length - 1) {
          tab.history = tab.history.sublist(0, tab.historyIndex + 1);
        }
        tab.history.add(target);
        tab.historyIndex = tab.history.length - 1;
      }
      tab.isSearching = false;
      tab.searchQuery = '';
      tab.isSelectionMode = false;
      tab.selectedPaths.clear();
      _searchController.clear();

      if (cached != null) {
        tab.entries = cached;
        _loading = false;
      }
    });
    _load(silent: cached != null);
  }

  void _navigateUp() {
    final tab = _currentTab;
    if (tab.atHome) return;
    if (tab.isSelectionMode) {
      setState(() {
        tab.isSelectionMode = false;
        tab.selectedPaths.clear();
      });
    }

    // If we are at the location root (or opened directly from Home), go straight to Home!
    if (tab.locationRoot != null && tab.path == tab.locationRoot) {
      _goToHome();
      return;
    }

    if (tab.isLocalDevice) {
      if (tab.path == _defaultLocalDevicePath || tab.path == '/' || tab.path.isEmpty) {
        _goToHome();
        return;
      }
      final parent = Directory(tab.path).parent.path;
      if (tab.locationRoot != null && (!parent.startsWith(tab.locationRoot!) && parent != tab.locationRoot)) {
        _goToHome();
        return;
      }
      _openPath(parent, isLocal: true);
      return;
    }

    if (tab.path == _defaultHomePath || tab.path == '/' || tab.path.isEmpty) {
      _goToHome();
      return;
    }

    final segments = tab.path.split('/').where((s) => s.isNotEmpty).toList();
    if (segments.length <= 1) {
      _goToHome();
    } else {
      segments.removeLast();
      final parent = '/${segments.join('/')}';
      if (tab.locationRoot != null && (tab.path == tab.locationRoot || (!parent.startsWith(tab.locationRoot!) && parent != tab.locationRoot))) {
        _goToHome();
      } else if (parent == '/DATA' || parent == '/DATA/Companion') {
        _goToHome();
      } else {
        _openPath(parent);
      }
    }
  }

  void _toggleFavorite() async {
    final tab = _currentTab;
    if (tab.atHome || tab.isLocalDevice) return;
    final path = tab.path;
    final name = tab.name;

    await ShortcutsService.instance.toggleFavorite(name, path);
    final isFav = await ShortcutsService.instance.isFavorited(path);
    if (mounted) {
      setState(() => _isCurrentPathFavorited = isFav);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(isFav ? 'Bookmarked "$name" to favorites.' : 'Removed "$name" from favorites.')),
      );
    }
    _loadFavorites();
  }

  void _showFavoritesSheet() {
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Center(child: Container(width: 36, height: 4, decoration: BoxDecoration(color: NivaroColors.borderHighlight, borderRadius: BorderRadius.circular(2)))),
              const SizedBox(height: 16),
              const Row(
                children: [
                  Icon(Icons.star_rounded, color: NivaroColors.warningLight, size: 22),
                  SizedBox(width: 10),
                  Text('Favorites & Bookmarks', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: Colors.white)),
                ],
              ),
              const SizedBox(height: 14),
              if (_favorites.isEmpty)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 20),
                  child: Center(child: Text('No favorite folders yet.', style: TextStyle(color: NivaroColors.textMuted))),
                )
              else
                Flexible(
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: _favorites.length,
                    separatorBuilder: (_, __) => const Divider(height: 1, color: NivaroColors.borderSubtle),
                    itemBuilder: (context, index) {
                      final fav = _favorites[index];
                      return ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: Container(
                          width: 36,
                          height: 36,
                          decoration: BoxDecoration(
                            color: NivaroColors.warning.withOpacity(0.14),
                            borderRadius: BorderRadius.circular(10),
                          ),
                          alignment: Alignment.center,
                          child: const Icon(Icons.folder_special_rounded, color: NivaroColors.warningLight, size: 20),
                        ),
                        title: Text(fav.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
                        subtitle: Text(fav.path, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5)),
                        onTap: () {
                          Navigator.pop(context);
                          _openPath(fav.path);
                        },
                      );
                    },
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  void _toggleSelection(String path) {
    final tab = _currentTab;
    setState(() {
      if (tab.selectedPaths.contains(path)) {
        tab.selectedPaths.remove(path);
        if (tab.selectedPaths.isEmpty) {
          tab.isSelectionMode = false;
        }
      } else {
        tab.selectedPaths.add(path);
        tab.isSelectionMode = true;
      }
    });
  }

  void _selectAll() {
    final tab = _currentTab;
    setState(() {
      tab.selectedPaths.addAll(_filteredEntries.map((e) => e.path));
      tab.isSelectionMode = true;
    });
  }

  void _clearSelection() {
    final tab = _currentTab;
    setState(() {
      tab.selectedPaths.clear();
      tab.isSelectionMode = false;
    });
  }

  void _copySelected() {
    final tab = _currentTab;
    setState(() {
      _clipboardPaths.clear();
      _clipboardPaths.addAll(tab.selectedPaths);
      _clipboardOp = 'copy';
      _clipboardIsLocal = tab.isLocalDevice;
      tab.isSelectionMode = false;
      tab.selectedPaths.clear();
    });
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Copied ${_clipboardPaths.length} items to clipboard.')));
  }

  void _cutSelected() {
    final tab = _currentTab;
    setState(() {
      _clipboardPaths.clear();
      _clipboardPaths.addAll(tab.selectedPaths);
      _clipboardOp = 'move';
      _clipboardIsLocal = tab.isLocalDevice;
      tab.isSelectionMode = false;
      tab.selectedPaths.clear();
    });
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Cut ${_clipboardPaths.length} items to clipboard.')));
  }

  Future<void> _pasteClipboard() async {
    if (_clipboardPaths.isEmpty) return;
    final tab = _currentTab;
    final total = _clipboardPaths.length;

    setState(() {
      _isTransferring = true;
      _transferTitle = _clipboardOp == 'move' ? 'Moving items...' : 'Copying items...';
      _transferTotalCount = total;
      _transferCurrentIndex = 0;
      _transferProgress = 0.0;
      _transferCancelled = false;
    });

    try {
      for (int i = 0; i < total; i++) {
        if (_transferCancelled) break;
        final srcPath = _clipboardPaths[i];
        final fileName = srcPath.split(Platform.pathSeparator).where((s) => s.isNotEmpty).lastOrNull ?? 'file';

        setState(() {
          _transferCurrentIndex = i + 1;
          _transferCurrentFile = fileName;
          _transferProgress = (i + 1) / total;
        });

        if (_clipboardIsLocal && !tab.isLocalDevice) {
          // Upload local phone file to server
          final auth = await ApiClient.instance.currentAuthHeader();
          final uri = Uri.parse('${ApiClient.instance.baseUrl}/v1/file?path=${tab.path}');
          final req = http.MultipartRequest('POST', uri);
          if (auth.isNotEmpty) req.headers['Authorization'] = auth;
          req.files.add(await http.MultipartFile.fromPath('file', srcPath, filename: fileName));
          final streamed = await req.send();
          if (streamed.statusCode >= 400) {
            throw Exception('Upload failed with HTTP ${streamed.statusCode}');
          }
        } else if (!_clipboardIsLocal && tab.isLocalDevice) {
          // Download server file to local phone storage
          final res = await ApiClient.instance.getRaw('/file', query: {'path': srcPath});
          if (res.statusCode == 200) {
            final destFile = File('${tab.path}/$fileName');
            await destFile.writeAsBytes(res.bodyBytes);
          } else {
            throw Exception('Download failed with HTTP ${res.statusCode}');
          }
        } else if (!_clipboardIsLocal && !tab.isLocalDevice) {
          // Server-to-server copy/move
          final endpoint = _clipboardOp == 'move' ? '/file/move' : '/file/copy';
          await ApiClient.instance.post(endpoint, body: {
            'from': srcPath,
            'to': '${tab.path}/$fileName',
          });
        } else {
          // Local-to-local copy/move
          final srcFile = File(srcPath);
          final destPath = '${tab.path}/$fileName';
          if (_clipboardOp == 'move') {
            await srcFile.rename(destPath);
          } else {
            await srcFile.copy(destPath);
          }
        }
      }

      if (_clipboardOp == 'move') {
        _clipboardPaths.clear();
      }

      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Completed transfer of $total items successfully.')),
        );
        _load();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Transfer failed: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _isTransferring = false);
      }
    }
  }

  Future<void> _deleteSelected() async {
    final tab = _currentTab;
    final count = tab.selectedPaths.length;
    if (count == 0) return;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: Text('Delete $count ${count == 1 ? "item" : "items"}?'),
        content: const Text('These files and folders will be permanently deleted.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    try {
      if (tab.isLocalDevice) {
        for (final p in tab.selectedPaths) {
          final f = File(p);
          if (f.existsSync()) {
            f.deleteSync(recursive: true);
          } else {
            final d = Directory(p);
            if (d.existsSync()) d.deleteSync(recursive: true);
          }
        }
      } else {
        final list = tab.selectedPaths.map((p) => {'path': p}).toList();
        try {
          await ApiClient.instance.deleteWithBody('/batch', list);
        } catch (_) {
          await ApiClient.instance.deleteWithBody('/file', list);
        }
      }
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Deleted $count ${count == 1 ? "item" : "items"}.'), backgroundColor: NivaroColors.success));
        tab.selectedPaths.clear();
        tab.isSelectionMode = false;
        _load();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Delete failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  Future<void> _deleteSingleEntry(FileEntry entry) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHighest,
        title: Text('Delete "${entry.name}"?'),
        content: Text('Are you sure you want to delete this ${entry.isDir ? "folder" : "file"}? This cannot be undone.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    try {
      if (_currentTab.isLocalDevice) {
        final f = File(entry.path);
        if (f.existsSync()) {
          f.deleteSync(recursive: true);
        } else {
          final d = Directory(entry.path);
          if (d.existsSync()) d.deleteSync(recursive: true);
        }
      } else {
        final list = [{'path': entry.path}];
        try {
          await ApiClient.instance.deleteWithBody('/batch', list);
        } catch (_) {
          await ApiClient.instance.deleteWithBody('/file', list);
        }
      }
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Deleted "${entry.name}".'), backgroundColor: NivaroColors.success));
        _load();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Delete failed: $e'), backgroundColor: NivaroColors.danger));
      }
    }
  }

  void _showEntryActions(FileEntry entry) {
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(width: 36, height: 4, decoration: BoxDecoration(color: NivaroColors.borderHighlight, borderRadius: BorderRadius.circular(2))),
              const SizedBox(height: 12),
              ListTile(
                leading: Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(
                    color: entry.isDir ? NivaroColors.warning.withOpacity(0.15) : NivaroColors.primary.withOpacity(0.15),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  alignment: Alignment.center,
                  child: Icon(
                    entry.isDir ? Icons.folder_rounded : Icons.insert_drive_file_rounded,
                    color: entry.isDir ? NivaroColors.warningLight : NivaroColors.primaryLight,
                    size: 22,
                  ),
                ),
                title: Text(entry.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15)),
                subtitle: Text(entry.isDir ? 'Folder' : formatBytes(entry.size), style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
              ),
              const Divider(height: 16),
              ListTile(
                leading: const Icon(Icons.check_circle_outline_rounded, color: NivaroColors.primaryLight),
                title: const Text('Select Item'),
                onTap: () {
                  Navigator.pop(context);
                  _toggleSelection(entry.path);
                },
              ),
              ListTile(
                leading: const Icon(Icons.copy_rounded, color: Colors.white70),
                title: const Text('Copy Item'),
                onTap: () {
                  Navigator.pop(context);
                  setState(() {
                    _clipboardPaths.clear();
                    _clipboardPaths.add(entry.path);
                    _clipboardOp = 'copy';
                    _clipboardIsLocal = _currentTab.isLocalDevice;
                  });
                  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Copied "${entry.name}". Navigate to target folder to paste.')));
                },
              ),
              ListTile(
                leading: const Icon(Icons.cut_rounded, color: Colors.white70),
                title: const Text('Cut / Move Item'),
                onTap: () {
                  Navigator.pop(context);
                  setState(() {
                    _clipboardPaths.clear();
                    _clipboardPaths.add(entry.path);
                    _clipboardOp = 'move';
                    _clipboardIsLocal = _currentTab.isLocalDevice;
                  });
                  ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('Cut "${entry.name}". Navigate to target folder to paste.')));
                },
              ),
              ListTile(
                leading: const Icon(Icons.delete_outline_rounded, color: NivaroColors.dangerLight),
                title: const Text('Delete', style: TextStyle(color: NivaroColors.dangerLight, fontWeight: FontWeight.w700)),
                onTap: () {
                  Navigator.pop(context);
                  _deleteSingleEntry(entry);
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  void _onEntryTap(FileEntry entry) {
    final tab = _currentTab;
    if (tab.isSelectionMode) {
      _toggleSelection(entry.path);
      return;
    }
    if (entry.isDir) {
      _openPath(entry.path, isLocal: tab.isLocalDevice);
    } else {
      Navigator.push(
        context,
        MaterialPageRoute(
          builder: (_) => FileViewerScreen(file: entry, path: entry.path, isLocal: tab.isLocalDevice),
        ),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final tab = _currentTab;
    final entries = _filteredEntries;

    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        backgroundColor: NivaroColors.surfaceDim,
        foregroundColor: Colors.white,
        elevation: 0,
        leading: tab.atHome
            ? null
            : IconButton(
                icon: const Icon(Icons.arrow_back_rounded),
                tooltip: 'Back',
                onPressed: _navigateUp,
              ),
        title: tab.isSearching
            ? TextField(
                controller: _searchController,
                autofocus: true,
                style: const TextStyle(color: Colors.white),
                decoration: const InputDecoration(
                  hintText: 'Search files & folders...',
                  border: InputBorder.none,
                ),
              )
            : Row(
                children: [
                  Icon(
                    tab.isLocalDevice ? Icons.phone_android_rounded : (tab.atHome ? Icons.dns_rounded : Icons.folder_rounded),
                    color: tab.isLocalDevice ? NivaroColors.primaryLight : (tab.atHome ? NivaroColors.primaryLight : NivaroColors.warningLight),
                    size: 20,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      tab.name,
                      style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 17),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
        actions: [
          IconButton(
            icon: Icon(tab.isSearching ? Icons.close_rounded : Icons.search_rounded),
            tooltip: tab.isSearching ? 'Close Search' : 'Search',
            onPressed: () {
              setState(() {
                tab.isSearching = !tab.isSearching;
                if (!tab.isSearching) {
                  tab.searchQuery = '';
                  _searchController.clear();
                }
              });
            },
          ),
          IconButton(
            icon: Badge(
              label: Text('${_tabs.length}'),
              isLabelVisible: _tabs.length > 1,
              child: const Icon(Icons.tab_rounded),
            ),
            tooltip: 'Tabs',
            onPressed: () => setState(() => _showTabsStrip = !_showTabsStrip),
          ),
          PopupMenuButton<String>(
            icon: const Icon(Icons.more_vert_rounded),
            tooltip: 'More Options',
            color: NivaroColors.surfaceContainerHighest,
            onSelected: (val) {
              switch (val) {
                case 'view_compact':
                  setState(() => tab.viewMode = 'compact');
                  break;
                case 'view_grid':
                  setState(() => tab.viewMode = 'grid');
                  break;
                case 'view_list':
                  setState(() => tab.viewMode = 'list');
                  break;
                case 'bookmark':
                  _toggleFavorite();
                  break;
                case 'all_favorites':
                  _showFavoritesSheet();
                  break;
                case 'local_storage':
                  _openLocalDeviceStorage();
                  break;
                case 'new_tab':
                  _addNewTab();
                  break;
                case 'refresh':
                  _load();
                  break;
                case 'hidden':
                  setState(() => tab.showHidden = !tab.showHidden);
                  break;
              }
            },
            itemBuilder: (context) => [
              const PopupMenuItem(
                enabled: false,
                child: Text('VIEW STYLE', style: TextStyle(color: NivaroColors.primaryLight, fontSize: 10, fontWeight: FontWeight.w800)),
              ),
              PopupMenuItem(
                value: 'view_compact',
                child: Row(
                  children: [
                    Icon(Icons.grid_view_rounded, size: 18, color: tab.viewMode == 'compact' ? NivaroColors.primaryLight : Colors.white70),
                    const SizedBox(width: 10),
                    Text('Thumbnail Grid (Compact)', style: TextStyle(color: tab.viewMode == 'compact' ? NivaroColors.primaryLight : Colors.white, fontWeight: tab.viewMode == 'compact' ? FontWeight.w700 : FontWeight.normal)),
                  ],
                ),
              ),
              PopupMenuItem(
                value: 'view_grid',
                child: Row(
                  children: [
                    Icon(Icons.view_module_rounded, size: 18, color: tab.viewMode == 'grid' ? NivaroColors.primaryLight : Colors.white70),
                    const SizedBox(width: 10),
                    Text('Large Cards Grid', style: TextStyle(color: tab.viewMode == 'grid' ? NivaroColors.primaryLight : Colors.white, fontWeight: tab.viewMode == 'grid' ? FontWeight.w700 : FontWeight.normal)),
                  ],
                ),
              ),
              PopupMenuItem(
                value: 'view_list',
                child: Row(
                  children: [
                    Icon(Icons.view_list_rounded, size: 18, color: tab.viewMode == 'list' ? NivaroColors.primaryLight : Colors.white70),
                    const SizedBox(width: 10),
                    Text('Detailed List', style: TextStyle(color: tab.viewMode == 'list' ? NivaroColors.primaryLight : Colors.white, fontWeight: tab.viewMode == 'list' ? FontWeight.w700 : FontWeight.normal)),
                  ],
                ),
              ),
              const PopupMenuDivider(),
              const PopupMenuItem(
                enabled: false,
                child: Text('BOOKMARKS & STORAGE', style: TextStyle(color: NivaroColors.primaryLight, fontSize: 10, fontWeight: FontWeight.w800)),
              ),
              if (!tab.atHome && !tab.isLocalDevice)
                PopupMenuItem(
                  value: 'bookmark',
                  child: Row(
                    children: [
                      Icon(_isCurrentPathFavorited ? Icons.star_rounded : Icons.star_border_rounded, size: 18, color: NivaroColors.warningLight),
                      const SizedBox(width: 10),
                      Text(_isCurrentPathFavorited ? 'Remove Bookmark' : 'Bookmark Folder'),
                    ],
                  ),
                ),
              const PopupMenuItem(
                value: 'all_favorites',
                child: Row(
                  children: [
                    Icon(Icons.bookmarks_rounded, size: 18, color: NivaroColors.warningLight),
                    SizedBox(width: 10),
                    Text('Favorites & Bookmarks'),
                  ],
                ),
              ),
              const PopupMenuItem(
                value: 'local_storage',
                child: Row(
                  children: [
                    Icon(Icons.phone_android_rounded, size: 18, color: NivaroColors.primaryLight),
                    SizedBox(width: 10),
                    Text('Browse Phone Storage'),
                  ],
                ),
              ),
              const PopupMenuDivider(),
              PopupMenuItem(
                value: 'hidden',
                child: Row(
                  children: [
                    Icon(tab.showHidden ? Icons.visibility_off_rounded : Icons.visibility_rounded, size: 18),
                    const SizedBox(width: 10),
                    Text(tab.showHidden ? 'Hide Hidden Files' : 'Show Hidden Files'),
                  ],
                ),
              ),
              const PopupMenuItem(
                value: 'new_tab',
                child: Row(
                  children: [
                    Icon(Icons.add_to_photos_rounded, size: 18),
                    SizedBox(width: 10),
                    Text('Open New Tab'),
                  ],
                ),
              ),
              const PopupMenuItem(
                value: 'refresh',
                child: Row(
                  children: [
                    Icon(Icons.refresh_rounded, size: 18),
                    SizedBox(width: 10),
                    Text('Refresh Directory'),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(width: 6),
        ],
      ),
      body: Stack(
        children: [
          Column(
            children: [
              if (_showTabsStrip || _tabs.length > 1) _buildTabsStrip(),
              if (_isTransferring) _buildTransferProgressBanner(),
              if (!tab.atHome) _buildCategoryFilterChips(),
              if (!tab.atHome) _buildBreadcrumbBar(),
              if (!tab.atHome && _findCompanionDeviceForPath(tab.path) != null)
                _buildCompanionBanner(_findCompanionDeviceForPath(tab.path)!),
              Expanded(
                child: tab.atHome
                    ? _buildStorageDashboard()
                    : (_loading
                        ? const Center(child: CircularProgressIndicator())
                        : (_error != null
                            ? _buildErrorView()
                            : (entries.isEmpty
                                ? _buildEmptyView()
                                : _buildEntriesView(entries)))),
              ),
            ],
          ),
          if (tab.isSelectionMode)
            Positioned(
              left: 16,
              right: 16,
              bottom: 84,
              child: _buildSelectionDock(),
            ),
          if (_clipboardPaths.isNotEmpty && !tab.isSelectionMode && !tab.atHome)
            Positioned(
              right: 16,
              bottom: 84,
              child: FloatingActionButton.extended(
                backgroundColor: NivaroColors.primary,
                foregroundColor: Colors.white,
                icon: const Icon(Icons.paste_rounded),
                label: Text('Paste (${_clipboardPaths.length})'),
                onPressed: _pasteClipboard,
              ),
            ),
        ],
      ),
    );
  }

  Widget _buildTransferProgressBanner() {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      color: NivaroColors.surfaceContainerLowest,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Row(
                children: [
                  const SizedBox(
                    width: 14,
                    height: 14,
                    child: CircularProgressIndicator(strokeWidth: 2, color: NivaroColors.primaryLight),
                  ),
                  const SizedBox(width: 10),
                  Text(_transferTitle, style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13, color: Colors.white)),
                ],
              ),
              Text('$_transferCurrentIndex of $_transferTotalCount', style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 12, color: NivaroColors.primaryLight)),
            ],
          ),
          const SizedBox(height: 6),
          ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: LinearProgressIndicator(
              value: _transferProgress,
              minHeight: 5,
              backgroundColor: NivaroColors.surfaceRaised,
              color: NivaroColors.primary,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            _transferCurrentFile,
            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
            overflow: TextOverflow.ellipsis,
          ),
        ],
      ),
    );
  }

  Widget _buildTabsStrip() {
    return Container(
      height: 42,
      color: NivaroColors.surfaceDim,
      child: Row(
        children: [
          Expanded(
            child: ListView.builder(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
              itemCount: _tabs.length,
              itemBuilder: (context, index) {
                final t = _tabs[index];
                final isSelected = index == _activeTabIndex;
                return Container(
                  margin: const EdgeInsets.only(right: 6),
                  decoration: BoxDecoration(
                    color: isSelected ? NivaroColors.surfaceRaised : Colors.transparent,
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(
                      color: isSelected ? NivaroColors.primary.withOpacity(0.5) : NivaroColors.borderSubtle,
                    ),
                  ),
                  child: InkWell(
                    onTap: () => _switchTab(index),
                    borderRadius: BorderRadius.circular(10),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 10),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Icon(
                            t.isLocalDevice ? Icons.phone_android_rounded : (t.atHome ? Icons.dns_rounded : Icons.folder_rounded),
                            size: 14,
                            color: isSelected ? NivaroColors.primaryLight : NivaroColors.textMuted,
                          ),
                          const SizedBox(width: 6),
                          Text(
                            t.name,
                            style: TextStyle(
                              color: isSelected ? Colors.white : NivaroColors.textSecondary,
                              fontWeight: isSelected ? FontWeight.w800 : FontWeight.normal,
                              fontSize: 12,
                            ),
                          ),
                          if (_tabs.length > 1) ...[
                            const SizedBox(width: 6),
                            InkWell(
                              onTap: () => _closeTab(index),
                              borderRadius: BorderRadius.circular(8),
                              child: const Padding(
                                padding: EdgeInsets.all(2),
                                child: Icon(Icons.close_rounded, size: 13, color: NivaroColors.textMuted),
                              ),
                            ),
                          ],
                        ],
                      ),
                    ),
                  ),
                );
              },
            ),
          ),
          IconButton(
            icon: const Icon(Icons.add_rounded, size: 18),
            tooltip: 'New Tab',
            visualDensity: VisualDensity.compact,
            onPressed: () => _addNewTab(),
          ),
        ],
      ),
    );
  }

  Widget _buildCategoryFilterChips() {
    final tab = _currentTab;
    final categories = [
      (FileCategoryFilter.all, 'All Files', Icons.dashboard_rounded),
      (FileCategoryFilter.folders, 'Folders', Icons.folder_rounded),
      (FileCategoryFilter.images, 'Images', Icons.image_rounded),
      (FileCategoryFilter.videos, 'Videos', Icons.videocam_rounded),
      (FileCategoryFilter.documents, 'Documents', Icons.description_rounded),
      (FileCategoryFilter.archives, 'Archives', Icons.archive_rounded),
      (FileCategoryFilter.code, 'Code', Icons.code_rounded),
    ];

    return Container(
      height: 38,
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: ListView.builder(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 12),
        itemCount: categories.length,
        itemBuilder: (context, index) {
          final cat = categories[index];
          final isSelected = tab.categoryFilter == cat.$1;
          return Padding(
            padding: const EdgeInsets.only(right: 6),
            child: FilterChip(
              avatar: Icon(cat.$3, size: 13, color: isSelected ? Colors.white : NivaroColors.textMuted),
              label: Text(cat.$2, style: TextStyle(fontSize: 11, fontWeight: isSelected ? FontWeight.w800 : FontWeight.w600)),
              selected: isSelected,
              showCheckmark: false,
              selectedColor: NivaroColors.primary,
              backgroundColor: NivaroColors.surfaceRaised,
              padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 0),
              visualDensity: VisualDensity.compact,
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14), side: BorderSide(color: isSelected ? NivaroColors.primaryLight : NivaroColors.borderSubtle)),
              onSelected: (_) {
                setState(() => tab.categoryFilter = cat.$1);
              },
            ),
          );
        },
      ),
    );
  }

  Widget _buildBreadcrumbBar() {
    final tab = _currentTab;
    final parts = tab.path.split('/').where((s) => s.isNotEmpty).toList();

    return Container(
      height: 36,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceDim,
        border: Border(bottom: BorderSide(color: NivaroColors.borderSubtle)),
      ),
      child: Row(
        children: [
          InkWell(
            onTap: () => _openPath(_defaultHomePath),
            child: const Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.dns_rounded, size: 14, color: NivaroColors.primaryLight),
                SizedBox(width: 4),
                Text('Root', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700, color: NivaroColors.primaryLight)),
              ],
            ),
          ),
          const SizedBox(width: 4),
          const Icon(Icons.chevron_right_rounded, size: 14, color: NivaroColors.textMuted),
          const SizedBox(width: 4),
          Expanded(
            child: ListView.builder(
              scrollDirection: Axis.horizontal,
              itemCount: parts.length,
              itemBuilder: (context, i) {
                final isLast = i == parts.length - 1;
                final subPath = '/${parts.sublist(0, i + 1).join('/')}';
                return Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    InkWell(
                      onTap: isLast ? null : () => _openPath(subPath, isLocal: tab.isLocalDevice),
                      child: Text(
                        parts[i],
                        style: TextStyle(
                          fontSize: 12,
                          fontWeight: isLast ? FontWeight.w800 : FontWeight.w600,
                          color: isLast ? Colors.white : NivaroColors.textSecondary,
                        ),
                      ),
                    ),
                    if (!isLast) ...[
                      const SizedBox(width: 4),
                      const Icon(Icons.chevron_right_rounded, size: 14, color: NivaroColors.textMuted),
                      const SizedBox(width: 4),
                    ],
                  ],
                );
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildStorageDashboard() {
    final screenWidth = MediaQuery.of(context).size.width;
    final isLandscape = MediaQuery.of(context).orientation == Orientation.landscape;
    final isTablet = screenWidth >= 600;
    final gridColumns = screenWidth >= 1200 ? 5 : (screenWidth >= 900 ? 4 : (isLandscape || isTablet ? 3 : 2));
    final gridRatio = screenWidth >= 900 ? 2.50 : 2.25;

    return RefreshIndicator(
      onRefresh: () async {
        await _loadFavorites();
        await _loadDisks();
        await _loadCloudAccounts();
        await _loadCompanionDevices();
      },
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 1150),
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 130),
            children: [
              // Section 1: Storage Devices & Drives
              Padding(
                padding: const EdgeInsets.only(bottom: 12, top: 4),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    const Text(
                      'Storage Devices & Drives',
                      style: TextStyle(fontWeight: FontWeight.w800, fontSize: 16, letterSpacing: -0.2, color: NivaroColors.textPrimary),
                    ),
                    if (_disksLoading)
                      const SizedBox(
                        width: 14,
                        height: 14,
                        child: CircularProgressIndicator(strokeWidth: 2, color: NivaroColors.primaryLight),
                      ),
                  ],
                ),
              ),
              if (_disks.isEmpty && !_disksLoading)
                Container(
                  padding: const EdgeInsets.all(16),
                  decoration: BoxDecoration(
                    color: NivaroColors.surfaceContainerLow,
                    borderRadius: BorderRadius.circular(16),
                    border: Border.all(color: NivaroColors.borderSubtle),
                  ),
                  child: const Row(
                    children: [
                      Icon(Icons.dns_outlined, color: NivaroColors.textMuted, size: 22),
                      SizedBox(width: 12),
                      Expanded(
                        child: Text(
                          'No server disks or storage pools connected',
                          style: TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                        ),
                      ),
                    ],
                  ),
                )
              else
                GridView(
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  padding: EdgeInsets.zero,
                  gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                    crossAxisCount: gridColumns,
                    crossAxisSpacing: 10,
                    mainAxisSpacing: 10,
                    childAspectRatio: gridRatio,
                  ),
                  children: [
                    for (final disk in _disks)
                      _buildTileCard(
                        icon: disk.isUsb ? Icons.usb_rounded : Icons.dns_rounded,
                        iconBgColor: disk.isUsb ? NivaroColors.accent.withOpacity(0.15) : NivaroColors.primary.withOpacity(0.15),
                        iconColor: disk.isUsb ? NivaroColors.accentLight : NivaroColors.primaryLight,
                        title: disk.label.isNotEmpty ? disk.label : disk.mountPoint,
                        subtitle: '${_formatBytes(disk.usedBytes)} / ${_formatBytes(disk.sizeBytes)}',
                        progress: disk.sizeBytes > 0 ? (disk.usedBytes / disk.sizeBytes).clamp(0.0, 1.0) : 0.0,
                        onTap: () => _openPath(disk.mountPoint),
                      ),
                  ],
                ),

              // Section 2: Connected Companion Devices (1 Column)
              if (_companionDevices.isNotEmpty || _companionLoading) ...[
                const SizedBox(height: 24),
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      const Text(
                        'Companion Devices',
                        style: TextStyle(fontWeight: FontWeight.w800, fontSize: 16, letterSpacing: -0.2, color: NivaroColors.textPrimary),
                      ),
                      if (_companionDevices.isNotEmpty)
                        Text(
                          '${_companionDevices.length} Connected',
                          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: NivaroColors.textMuted),
                        ),
                    ],
                  ),
                ),
                if (_companionLoading)
                  const Center(child: Padding(padding: EdgeInsets.all(16), child: CircularProgressIndicator()))
                else
                  ListView.separated(
                    shrinkWrap: true,
                    physics: const NeverScrollableScrollPhysics(),
                    padding: EdgeInsets.zero,
                    itemCount: _companionDevices.length,
                    separatorBuilder: (_, __) => const SizedBox(height: 10),
                    itemBuilder: (context, index) {
                      final dev = _companionDevices[index];
                      return _buildCompanionCard1Col(dev);
                    },
                  ),
              ],

              const SizedBox(height: 24),

              // Section 3: Cloud Storage Accounts
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    const Text(
                      'Cloud Storage',
                      style: TextStyle(fontWeight: FontWeight.w800, fontSize: 16, letterSpacing: -0.2, color: NivaroColors.textPrimary),
                    ),
                    if (_cloudAccounts.isNotEmpty)
                      Text(
                        '${_cloudAccounts.length} Connected',
                        style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: NivaroColors.textMuted),
                      ),
                  ],
                ),
              ),
              if (_cloudLoading)
                const Center(child: Padding(padding: EdgeInsets.all(16), child: CircularProgressIndicator()))
              else if (_cloudAccounts.isEmpty)
                _buildEmptyCloudCard()
              else
                GridView.builder(
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  padding: EdgeInsets.zero,
                  gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                    crossAxisCount: gridColumns,
                    crossAxisSpacing: 10,
                    mainAxisSpacing: 10,
                    childAspectRatio: gridRatio,
                  ),
                  itemCount: _cloudAccounts.length,
                  itemBuilder: (context, index) {
                    final account = _cloudAccounts[index];
                    final iconData = _getCloudIcon(account.type);
                    final iconColor = _getCloudColor(account.type);

                    return _buildTileCard(
                      icon: iconData,
                      iconBgColor: iconColor.withOpacity(0.15),
                      iconColor: iconColor,
                      title: account.displayName,
                      subtitle: account.mountPoint.isNotEmpty ? account.mountPoint : account.providerTitle,
                      badgeText: account.downloadMbps != null ? '${account.downloadMbps!.toStringAsFixed(0)}M' : null,
                      badgeColor: NivaroColors.success.withOpacity(0.15),
                      badgeTextColor: NivaroColors.successLight,
                      onTap: () {
                        if (account.mountPoint.isNotEmpty) {
                          _openPath(account.mountPoint);
                        } else {
                          _showCloudAccountDetails(account);
                        }
                      },
                    );
                  },
                ),

              const SizedBox(height: 24),

              // Section 4: Favorite Folders
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    const Text(
                      'Favorite Folders',
                      style: TextStyle(fontWeight: FontWeight.w800, fontSize: 16, letterSpacing: -0.2, color: NivaroColors.textPrimary),
                    ),
                    if (_favorites.isNotEmpty)
                      InkWell(
                        onTap: _showFavoritesSheet,
                        borderRadius: BorderRadius.circular(8),
                        child: const Padding(
                          padding: EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                          child: Text('View All', style: TextStyle(color: NivaroColors.primaryLight, fontSize: 12, fontWeight: FontWeight.w700)),
                        ),
                      ),
                  ],
                ),
              ),
              if (_favoritesLoading)
                const Center(child: Padding(padding: EdgeInsets.all(16), child: CircularProgressIndicator()))
              else if (_favorites.isEmpty)
                const Padding(
                  padding: EdgeInsets.all(12),
                  child: Text('No favorite folders yet. Use the 3-dot menu in any folder to bookmark.', style: TextStyle(color: NivaroColors.textMuted, fontSize: 13)),
                )
              else
                GridView.builder(
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  padding: EdgeInsets.zero,
                  gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                    crossAxisCount: gridColumns,
                    crossAxisSpacing: 10,
                    mainAxisSpacing: 10,
                    childAspectRatio: gridRatio,
                  ),
                  itemCount: _favorites.length.clamp(0, 6),
                  itemBuilder: (context, index) {
                    final fav = _favorites[index];
                    return _buildTileCard(
                      icon: Icons.folder_special_rounded,
                      iconBgColor: NivaroColors.warning.withOpacity(0.15),
                      iconColor: NivaroColors.warningLight,
                      title: fav.name,
                      subtitle: fav.path,
                      onTap: () => _openPath(fav.path),
                    );
                  },
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildTileCard({
    required IconData icon,
    required Color iconBgColor,
    required Color iconColor,
    required String title,
    required String subtitle,
    String? badgeText,
    Color? badgeColor,
    Color? badgeTextColor,
    double? progress,
    VoidCallback? onTap,
  }) {
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: () {
          onTap?.call();
        },
        borderRadius: BorderRadius.circular(16),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          decoration: BoxDecoration(
            color: NivaroColors.surfaceContainerLow,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(color: NivaroColors.borderSubtle),
            boxShadow: const [
              BoxShadow(color: Color(0x1F000000), blurRadius: 6, offset: Offset(0, 2)),
            ],
          ),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 36,
                    height: 36,
                    decoration: BoxDecoration(
                      color: iconBgColor,
                      borderRadius: BorderRadius.circular(10),
                    ),
                    alignment: Alignment.center,
                    child: Icon(icon, color: iconColor, size: 20),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Row(
                          children: [
                            Expanded(
                              child: Text(
                                title,
                                style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13, color: NivaroColors.textPrimary),
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                            if (badgeText != null) ...[
                              const SizedBox(width: 4),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 1.5),
                                decoration: BoxDecoration(
                                  color: badgeColor ?? NivaroColors.primary.withOpacity(0.12),
                                  borderRadius: BorderRadius.circular(4),
                                ),
                                child: Text(
                                  badgeText,
                                  style: TextStyle(
                                    color: badgeTextColor ?? NivaroColors.primaryLight,
                                    fontSize: 9.5,
                                    fontWeight: FontWeight.w800,
                                  ),
                                ),
                              ),
                            ],
                          ],
                        ),
                        const SizedBox(height: 2),
                        Text(
                          subtitle,
                          style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              if (progress != null) ...[
                const SizedBox(height: 6),
                ClipRRect(
                  borderRadius: BorderRadius.circular(2),
                  child: LinearProgressIndicator(
                    value: progress.clamp(0.0, 1.0),
                    backgroundColor: NivaroColors.surfaceRaised,
                    valueColor: AlwaysStoppedAnimation<Color>(
                      progress > 0.90 ? NivaroColors.dangerLight : (progress > 0.80 ? NivaroColors.warningLight : NivaroColors.primaryLight),
                    ),
                    minHeight: 3,
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildEmptyCloudCard() {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: NivaroColors.surfaceContainerLow,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: NivaroColors.borderSubtle),
      ),
      child: Row(
        children: [
          Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(
              color: NivaroColors.primary.withOpacity(0.12),
              borderRadius: BorderRadius.circular(10),
            ),
            alignment: Alignment.center,
            child: const Icon(Icons.cloud_queue_rounded, color: NivaroColors.primaryLight, size: 20),
          ),
          const SizedBox(width: 12),
          const Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('No Cloud Drives Connected', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13, color: NivaroColors.textPrimary)),
                SizedBox(height: 2),
                Text('Sync Google Drive, OneDrive, Nextcloud or S3 via Web UI / Cloud settings.', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11)),
              ],
            ),
          ),
        ],
      ),
    );
  }

  IconData _getCloudIcon(String type) {
    switch (type.toLowerCase()) {
      case 'drive':
        return Icons.add_to_drive_rounded;
      case 'onedrive':
        return Icons.cloud_queue_rounded;
      case 'dropbox':
        return Icons.folder_shared_rounded;
      case 'nextcloud':
        return Icons.cloud_sync_rounded;
      case 's3':
        return Icons.storage_rounded;
      default:
        return Icons.cloud_rounded;
    }
  }

  Color _getCloudColor(String type) {
    switch (type.toLowerCase()) {
      case 'drive':
        return NivaroColors.warningLight;
      case 'onedrive':
        return NivaroColors.primaryLight;
      case 'dropbox':
        return NivaroColors.infoLight;
      case 'nextcloud':
        return NivaroColors.cyanLight;
      case 's3':
        return NivaroColors.warningLight;
      default:
        return NivaroColors.primaryLight;
    }
  }

  void _showCloudAccountDetails(CloudAccount account) {
    showModalBottomSheet(
      context: context,
      backgroundColor: NivaroColors.surfaceContainerHighest,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Center(child: Container(width: 36, height: 4, decoration: BoxDecoration(color: NivaroColors.borderHighlight, borderRadius: BorderRadius.circular(2)))),
              const SizedBox(height: 16),
              Row(
                children: [
                  Icon(_getCloudIcon(account.type), color: _getCloudColor(account.type), size: 24),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(account.displayName, style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 17, color: Colors.white)),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              ListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Provider Type', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                subtitle: Text(account.providerTitle, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14, color: Colors.white)),
              ),
              if (account.mountPoint.isNotEmpty)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Mount Path', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                  subtitle: Text(account.mountPoint, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14, color: NivaroColors.primaryLight)),
                  trailing: const Icon(Icons.arrow_forward_ios_rounded, size: 14, color: NivaroColors.textMuted),
                  onTap: () {
                    Navigator.pop(context);
                    _openPath(account.mountPoint);
                  },
                ),
              if (account.downloadMbps != null)
                ListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Transfer Speed', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                  subtitle: Text('Download: ${account.downloadMbps!.toStringAsFixed(1)} Mbps | Upload: ${account.uploadMbps?.toStringAsFixed(1) ?? "--"} Mbps', style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14, color: Colors.white)),
                ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildEntriesView(List<FileEntry> entries) {
    final mode = _currentTab.viewMode;
    if (mode == 'compact') {
      return _buildCompactThumbnailGrid(entries);
    } else if (mode == 'grid') {
      return _buildLargeCardsGrid(entries);
    } else {
      return _buildDetailedListView(entries);
    }
  }

  Widget _buildCompactThumbnailGrid(List<FileEntry> entries) {
    final width = MediaQuery.of(context).size.width;
    final isLandscape = MediaQuery.of(context).orientation == Orientation.landscape;
    final cols = width >= 1200 ? 10 : (width >= 900 ? 8 : (width >= 600 || isLandscape ? 6 : (width >= 400 ? 4 : 3)));
    final ratio = width >= 600 ? 0.90 : 0.82;
    return GridView.builder(
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 130),
      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: cols,
        crossAxisSpacing: 10,
        mainAxisSpacing: 10,
        childAspectRatio: ratio,
      ),
      itemCount: entries.length,
      itemBuilder: (context, index) {
        final entry = entries[index];
        final isSelected = _currentTab.selectedPaths.contains(entry.path);

        return InkWell(
          onTap: () => _onEntryTap(entry),
          onLongPress: () => _toggleSelection(entry.path),
          borderRadius: BorderRadius.circular(16),
          child: Container(
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(
              color: isSelected ? NivaroColors.primary.withOpacity(0.18) : NivaroColors.surfaceContainerLow,
              borderRadius: BorderRadius.circular(16),
              border: Border.all(
                color: isSelected ? NivaroColors.primaryLight : NivaroColors.borderSubtle,
                width: isSelected ? 1.5 : 1.0,
              ),
            ),
            child: Column(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Stack(
                  alignment: Alignment.topRight,
                  children: [
                    Container(
                      width: 52,
                      height: 52,
                      decoration: BoxDecoration(
                        color: entry.isDir ? NivaroColors.warning.withOpacity(0.12) : NivaroColors.primary.withOpacity(0.12),
                        borderRadius: BorderRadius.circular(14),
                      ),
                      child: Icon(
                        entry.isDir ? Icons.folder_rounded : (entry.isImage ? Icons.image_rounded : (entry.isVideo ? Icons.videocam_rounded : Icons.insert_drive_file_rounded)),
                        color: entry.isDir ? NivaroColors.warningLight : (entry.isImage ? NivaroColors.infoLight : NivaroColors.primaryLight),
                        size: 28,
                      ),
                    ),
                    if (isSelected)
                      const CircleAvatar(
                        radius: 8,
                        backgroundColor: NivaroColors.primary,
                        child: Icon(Icons.check, size: 11, color: Colors.white),
                      ),
                  ],
                ),
                const SizedBox(height: 8),
                Text(
                  entry.name,
                  style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 12, color: Colors.white),
                  maxLines: 2,
                  textAlign: TextAlign.center,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: 2),
                Text(
                  entry.isDir ? 'Folder' : formatBytes(entry.size),
                  style: const TextStyle(color: NivaroColors.textMuted, fontSize: 10),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _buildLargeCardsGrid(List<FileEntry> entries) {
    final width = MediaQuery.of(context).size.width;
    final isLandscape = MediaQuery.of(context).orientation == Orientation.landscape;
    final cols = width >= 1200 ? 8 : (width >= 900 ? 6 : (width >= 600 || isLandscape ? 4 : 2));
    final ratio = width >= 900 ? 1.38 : (width >= 600 || isLandscape ? 1.32 : 1.25);
    return GridView.builder(
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 130),
      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: cols,
        crossAxisSpacing: 10,
        mainAxisSpacing: 10,
        childAspectRatio: ratio,
      ),
      itemCount: entries.length,
      itemBuilder: (context, index) {
        final entry = entries[index];
        final isSelected = _currentTab.selectedPaths.contains(entry.path);

        return InkWell(
          onTap: () => _onEntryTap(entry),
          onLongPress: () => _toggleSelection(entry.path),
          borderRadius: BorderRadius.circular(16),
          child: Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: isSelected ? NivaroColors.primary.withOpacity(0.18) : NivaroColors.surfaceContainerLow,
              borderRadius: BorderRadius.circular(16),
              border: Border.all(
                color: isSelected ? NivaroColors.primaryLight : NivaroColors.borderSubtle,
                width: isSelected ? 1.5 : 1.0,
              ),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Row(
                  mainAxisAlignment: MainAxisAlignment.spaceBetween,
                  children: [
                    Icon(
                      entry.isDir ? Icons.folder_rounded : Icons.insert_drive_file_rounded,
                      color: entry.isDir ? NivaroColors.warningLight : NivaroColors.primaryLight,
                      size: 32,
                    ),
                    if (isSelected)
                      const CircleAvatar(
                        radius: 10,
                        backgroundColor: NivaroColors.primary,
                        child: Icon(Icons.check, size: 12, color: Colors.white),
                      ),
                  ],
                ),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      entry.name,
                      style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13, color: Colors.white),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    const SizedBox(height: 2),
                    Text(
                      entry.isDir ? 'Folder' : formatBytes(entry.size),
                      style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
                    ),
                  ],
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _buildDetailedListView(List<FileEntry> entries) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 1100),
        child: ListView.separated(
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 130),
      itemCount: entries.length,
      separatorBuilder: (_, __) => const Divider(height: 1, color: NivaroColors.borderSubtle),
      itemBuilder: (context, index) {
        final entry = entries[index];
        final isSelected = _currentTab.selectedPaths.contains(entry.path);

        return ListTile(
          onTap: () => _onEntryTap(entry),
          onLongPress: () => _toggleSelection(entry.path),
          selected: isSelected,
          selectedTileColor: NivaroColors.primary.withOpacity(0.15),
          leading: Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(
              color: entry.isDir ? NivaroColors.warning.withOpacity(0.12) : NivaroColors.primary.withOpacity(0.12),
              borderRadius: BorderRadius.circular(10),
            ),
            alignment: Alignment.center,
            child: Icon(
              entry.isDir ? Icons.folder_rounded : Icons.insert_drive_file_rounded,
              color: entry.isDir ? NivaroColors.warningLight : NivaroColors.primaryLight,
              size: 20,
            ),
          ),
          title: Text(entry.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
          subtitle: Text(
            entry.isDir ? 'Folder' : formatBytes(entry.size),
            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
          ),
          trailing: isSelected
              ? const Icon(Icons.check_circle_rounded, color: NivaroColors.primaryLight)
              : IconButton(
                  icon: const Icon(Icons.more_vert_rounded, color: NivaroColors.textMuted, size: 20),
                  tooltip: 'Options',
                  onPressed: () => _showEntryActions(entry),
                ),
        );
      },
    ),
  ),
);
}

  Widget _buildSelectionDock() {
    final count = _currentTab.selectedPaths.length;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: NivaroColors.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: NivaroColors.primaryLight.withOpacity(0.6), width: 1.5),
        boxShadow: const [
          BoxShadow(color: Color(0xAA000000), blurRadius: 16, offset: Offset(0, 4)),
        ],
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Row(
            children: [
              IconButton(
                icon: const Icon(Icons.close_rounded, size: 20),
                onPressed: _clearSelection,
                visualDensity: VisualDensity.compact,
              ),
              const SizedBox(width: 4),
              Text('$count Selected', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 14, color: Colors.white)),
            ],
          ),
          Row(
            children: [
              IconButton(
                icon: const Icon(Icons.select_all_rounded, size: 20),
                tooltip: 'Select All',
                onPressed: _selectAll,
              ),
              IconButton(
                icon: const Icon(Icons.copy_rounded, size: 20),
                tooltip: 'Copy',
                onPressed: _copySelected,
              ),
              IconButton(
                icon: const Icon(Icons.cut_rounded, size: 20),
                tooltip: 'Cut / Move',
                onPressed: _cutSelected,
              ),
              IconButton(
                icon: const Icon(Icons.delete_rounded, size: 20, color: NivaroColors.dangerLight),
                tooltip: 'Delete',
                onPressed: _deleteSelected,
              ),
            ],
          ),
        ],
      ),
    );
  }

  IconData _getCompanionIcon(CompanionDevice dev) {
    final modelLower = dev.model.toLowerCase();
    final nameLower = dev.name.toLowerCase();
    final platformLower = dev.platform.toLowerCase();
    final isTablet = modelLower.contains('tablet') ||
        modelLower.contains('pad') ||
        modelLower.contains('tab') ||
        modelLower.contains('ruan') ||
        nameLower.contains('tablet') ||
        nameLower.contains('pad') ||
        nameLower.contains('tab') ||
        platformLower.contains('ipad');
    final isIos = platformLower.contains('ios') ||
        platformLower.contains('iphone') ||
        nameLower.contains('iphone');
    return isIos ? Icons.phone_iphone_rounded : (isTablet ? Icons.tablet_android_rounded : Icons.phone_android_rounded);
  }

  IconData _getBatteryIcon(int level) {
    if (level >= 90) return Icons.battery_full_rounded;
    if (level >= 75) return Icons.battery_6_bar_rounded;
    if (level >= 50) return Icons.battery_5_bar_rounded;
    if (level >= 30) return Icons.battery_3_bar_rounded;
    if (level >= 15) return Icons.battery_2_bar_rounded;
    return Icons.battery_alert_rounded;
  }

  Widget _buildCompanionCard1Col(CompanionDevice dev) {
    final devIcon = _getCompanionIcon(dev);
    final usedStr = _formatBytes(dev.usedStorageBytes);
    final totalStr = _formatBytes(dev.totalStorageBytes);
    final pctStr = dev.totalStorageBytes > 0 ? '${(dev.storageUsagePercent * 100).round()}%' : '0%';

    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: () {
          if (dev.isCurrentDevice) {
            _openLocalDeviceStorage();
          } else {
            final targetPath = dev.storagePath.isNotEmpty ? dev.storagePath : '/DATA/Companion/${dev.name}';
            _openPath(targetPath);
          }
        },
        borderRadius: BorderRadius.circular(16),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
          decoration: BoxDecoration(
            color: NivaroColors.surfaceContainerLow,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(
              color: dev.isCurrentDevice ? NivaroColors.primary.withOpacity(0.4) : NivaroColors.borderSubtle,
              width: dev.isCurrentDevice ? 1.2 : 1.0,
            ),
            boxShadow: const [
              BoxShadow(color: Color(0x1F000000), blurRadius: 6, offset: Offset(0, 2)),
            ],
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 38,
                    height: 38,
                    decoration: BoxDecoration(
                      color: dev.isCurrentDevice
                          ? NivaroColors.primary.withOpacity(0.15)
                          : NivaroColors.purple.withOpacity(0.15),
                      borderRadius: BorderRadius.circular(10),
                    ),
                    alignment: Alignment.center,
                    child: Icon(
                      devIcon,
                      color: dev.isCurrentDevice ? NivaroColors.primaryLight : NivaroColors.purpleLight,
                      size: 22,
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Flexible(
                              child: Text(
                                dev.name,
                                style: const TextStyle(
                                  fontWeight: FontWeight.w700,
                                  fontSize: 13.5,
                                  color: NivaroColors.textPrimary,
                                ),
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                            const SizedBox(width: 6),
                            Container(
                              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                              decoration: BoxDecoration(
                                color: dev.isCurrentDevice
                                    ? NivaroColors.primary.withOpacity(0.15)
                                    : (dev.isOnline ? NivaroColors.success.withOpacity(0.15) : Colors.white10),
                                borderRadius: BorderRadius.circular(4),
                              ),
                              child: Text(
                                dev.isCurrentDevice ? 'This Device' : (dev.isOnline ? 'Online' : 'Offline'),
                                style: TextStyle(
                                  color: dev.isCurrentDevice
                                      ? NivaroColors.primaryLight
                                      : (dev.isOnline ? NivaroColors.successLight : NivaroColors.textMuted),
                                  fontSize: 10,
                                  fontWeight: FontWeight.w800,
                                ),
                              ),
                            ),
                            if (dev.batteryLevel > 0) ...[
                              const SizedBox(width: 6),
                              Row(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                                  Icon(
                                    _getBatteryIcon(dev.batteryLevel),
                                    size: 13,
                                    color: dev.batteryLevel <= 20 ? NivaroColors.dangerLight : NivaroColors.textMuted,
                                  ),
                                  const SizedBox(width: 2),
                                  Text(
                                    '${dev.batteryLevel}%',
                                    style: TextStyle(
                                      color: dev.batteryLevel <= 20 ? NivaroColors.dangerLight : NivaroColors.textMuted,
                                      fontSize: 10.5,
                                      fontWeight: FontWeight.w600,
                                    ),
                                  ),
                                ],
                              ),
                            ],
                          ],
                        ),
                        const SizedBox(height: 3),
                        Row(
                          children: [
                            Text(
                              '$usedStr / $totalStr ($pctStr)',
                              style: const TextStyle(
                                color: NivaroColors.textMuted,
                                fontSize: 11.5,
                                fontWeight: FontWeight.w500,
                              ),
                            ),
                            if (dev.serverStorageUsed > 0) ...[
                              const SizedBox(width: 6),
                              Text(
                                '· ${_formatBytes(dev.serverStorageUsed)} synced',
                                style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
                              ),
                            ],
                          ],
                        ),
                      ],
                    ),
                  ),
                  const Icon(Icons.chevron_right_rounded, color: NivaroColors.textMuted, size: 20),
                ],
              ),
              const SizedBox(height: 8),
              ClipRRect(
                borderRadius: BorderRadius.circular(2),
                child: LinearProgressIndicator(
                  value: dev.storageUsagePercent,
                  backgroundColor: NivaroColors.surfaceRaised,
                  valueColor: AlwaysStoppedAnimation<Color>(
                    dev.storageUsagePercent > 0.90
                        ? NivaroColors.dangerLight
                        : (dev.storageUsagePercent > 0.80
                            ? NivaroColors.warningLight
                            : (dev.isCurrentDevice ? NivaroColors.primaryLight : NivaroColors.purpleLight)),
                  ),
                  minHeight: 3.5,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  CompanionDevice? _findCompanionDeviceForPath(String path) {
    for (final dev in _companionDevices) {
      if (dev.storagePath.isNotEmpty && path.startsWith(dev.storagePath)) {
        return dev;
      }
      final cleanName = dev.name.toLowerCase().replaceAll(RegExp(r'[^a-z0-9]'), '_');
      if (path.toLowerCase().contains(cleanName) || path.toLowerCase().contains(dev.name.toLowerCase())) {
        return dev;
      }
    }
    return null;
  }

  Widget _buildCompanionBanner(CompanionDevice dev) {
    final usedStr = _formatBytes(dev.usedStorageBytes);
    final totalStr = _formatBytes(dev.totalStorageBytes);
    final storageRatio = dev.storageUsagePercent;

    return Container(
      margin: const EdgeInsets.fromLTRB(14, 4, 14, 10),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: NivaroColors.surfaceContainerLow,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: NivaroColors.borderSubtle),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(
                  color: NivaroColors.purple.withOpacity(0.15),
                  borderRadius: BorderRadius.circular(10),
                ),
                child: Icon(_getCompanionIcon(dev), color: NivaroColors.purpleLight, size: 22),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            dev.name,
                            style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 14.5, color: Colors.white),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        const SizedBox(width: 8),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                          decoration: BoxDecoration(
                            color: dev.isOnline ? NivaroColors.success.withOpacity(0.15) : Colors.white10,
                            borderRadius: BorderRadius.circular(6),
                          ),
                          child: Text(
                            dev.isOnline ? 'ONLINE' : 'OFFLINE',
                            style: TextStyle(
                              color: dev.isOnline ? NivaroColors.successLight : NivaroColors.textMuted,
                              fontSize: 9,
                              fontWeight: FontWeight.w800,
                            ),
                          ),
                        ),
                        if (dev.batteryLevel > 0) ...[
                          const SizedBox(width: 6),
                          Text(
                            '${dev.batteryLevel}%',
                            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11, fontWeight: FontWeight.w600),
                          ),
                        ],
                      ],
                    ),
                    const SizedBox(height: 2),
                    Text(
                      '${dev.model} · ${dev.platform} · Companion Sync Folder',
                      style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Text('Device Storage', style: TextStyle(fontWeight: FontWeight.w600, fontSize: 11.5, color: Colors.white70)),
              Text('$usedStr / $totalStr', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 11.5, color: NivaroColors.primaryLight)),
            ],
          ),
          const SizedBox(height: 6),
          ClipRRect(
            borderRadius: BorderRadius.circular(3),
            child: LinearProgressIndicator(
              value: storageRatio,
              minHeight: 5,
              backgroundColor: NivaroColors.surfaceRaised,
              valueColor: AlwaysStoppedAnimation<Color>(
                storageRatio > 0.90 ? NivaroColors.dangerLight : (storageRatio > 0.80 ? NivaroColors.warningLight : NivaroColors.primaryLight),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildEmptyView() {
    final compDev = _findCompanionDeviceForPath(_currentTab.path);
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(compDev != null ? _getCompanionIcon(compDev) : Icons.folder_open_rounded, size: 48, color: NivaroColors.textMuted),
          const SizedBox(height: 12),
          Text(
            compDev != null ? 'No files shared yet with ${compDev.name}.' : 'This folder is empty.',
            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 14),
          ),
          if (compDev != null) ...[
            const SizedBox(height: 6),
            const Text(
              'Files placed in this folder will sync with this companion device.',
              style: TextStyle(color: NivaroColors.textMuted, fontSize: 12),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildErrorView() {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.error_outline_rounded, size: 44, color: NivaroColors.dangerLight),
            const SizedBox(height: 12),
            Text(_error ?? 'Could not load folder contents.', style: const TextStyle(color: Colors.white, fontSize: 14), textAlign: TextAlign.center),
            const SizedBox(height: 16),
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
                onPressed: () => _load(),
                child: const Text('Try Again', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12.5)),
              ),
            ] else
              ElevatedButton(
                onPressed: () => _load(),
                child: const Text('Try Again'),
              ),
          ],
        ),
      ),
    );
  }
}
