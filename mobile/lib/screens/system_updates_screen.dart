import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:open_filex/open_filex.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path_provider/path_provider.dart';
import 'package:url_launcher/url_launcher.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';

/// Updates Center - deliberately TWO independent tracks, never mixed:
///
/// - Server: this NivaroOS box's own backend/services, checked via
///   GET /v1/sys/version/check and applied via POST /v1/sys/update - both
///   real, server-authoritative endpoints (services/core/route/v1/system.go),
///   nothing to do with GitHub or this app.
/// - App: this Flutter app's own installed version (PackageInfo, the actual
///   build you're running) vs. the latest GitHub Release's tag + attached
///   APK - entirely separate from the server's version.
class SystemUpdatesScreen extends StatefulWidget {
  const SystemUpdatesScreen({super.key});

  @override
  State<SystemUpdatesScreen> createState() => _SystemUpdatesScreenState();
}

class _SystemUpdatesScreenState extends State<SystemUpdatesScreen> {
  // --- Server track ---
  String _serverCurrentVersion = '';
  String _serverLatestVersion = '';
  String _serverChangeLog = '';
  bool _serverChecking = true;
  bool _serverNeedUpdate = false;
  bool _serverUpdating = false;
  int _upgradablePackages = 0;
  int _securityCount = 0;
  List<Map<String, dynamic>> _packageList = [];
  bool _refreshingRepos = false;
  bool _upgradingApt = false;
  String? _serverStatusMessage;
  String _lastServerCheckedTime = '';

  // --- App track ---
  String _appInstalledVersion = '';
  String _appLatestVersion = '';
  String _appReleaseNotes = '';
  String? _appApkUrl;
  int _appApkSizeBytes = 0;
  bool _appChecking = true;
  bool _appHasUpdate = false;
  bool _appDownloading = false;
  double _appDownloadProgress = 0.0;

  Map<String, dynamic>? _latestCommit;

  @override
  void initState() {
    super.initState();
    _checkServerUpdate();
    _checkAppUpdate();
  }

  static String _cleanVersion(String v) {
    return v.trim().replaceFirst(RegExp(r'^[vV]'), '');
  }

  static bool _isNewer(String latest, String current) {
    final l = _cleanVersion(latest);
    final c = _cleanVersion(current);
    if (l.isEmpty || c.isEmpty) return false;
    final lParts = l.split('.').map((p) => int.tryParse(p) ?? 0).toList();
    final cParts = c.split('.').map((p) => int.tryParse(p) ?? 0).toList();
    while (lParts.length < 3) {
      lParts.add(0);
    }
    while (cParts.length < 3) {
      cParts.add(0);
    }
    for (int i = 0; i < 3; i++) {
      if (lParts[i] > cParts[i]) return true;
      if (lParts[i] < cParts[i]) return false;
    }
    return false;
  }

  Future<void> _checkServerUpdate() async {
    setState(() {
      _serverChecking = true;
      _serverStatusMessage = null;
    });

    String currVer = _serverCurrentVersion;
    String latVer = _serverCurrentVersion;
    bool needUpdate = false;
    String changeLog = '';

    try {
      final res = await ApiClient.instance.get('/sys/version/check');
      if (res['data'] is Map) {
        final d = res['data'] as Map<String, dynamic>;
        if (d['current_version'] != null &&
            d['current_version'].toString().isNotEmpty) {
          currVer = d['current_version'].toString();
          if (!currVer.startsWith('v')) currVer = 'v$currVer';
        }
        needUpdate = d['need_update'] == true;
        if (d['version'] is Map) {
          final vMap = d['version'] as Map<String, dynamic>;
          if (vMap['version'] != null &&
              vMap['version'].toString().isNotEmpty) {
            latVer = vMap['version'].toString();
            if (!latVer.startsWith('v')) latVer = 'v$latVer';
          }
          // The real field is "change_log" (services/core/model/version.go) -
          // this used to check "release_notes"/"changelog", neither of which
          // ever matched, so real server changelog text was silently dropped.
          if (vMap['change_log'] != null) {
            changeLog = vMap['change_log'].toString();
          }
        }
      }
    } catch (e) {
      debugPrint('[SystemUpdates] Server version check error: $e');
    }

    int pkgUpgrades = 0;
    int secUpgrades = 0;
    List<Map<String, dynamic>> pkgs = [];
    try {
      final pkgRes = await ApiClient.instance.get('/sys/packages/check');
      if (pkgRes['data'] is Map) {
        final d = pkgRes['data'] as Map<String, dynamic>;
        pkgUpgrades = d['count'] as int? ?? 0;
        secUpgrades = d['security_count'] as int? ?? 0;
        if (d['packages'] is List) {
          pkgs = (d['packages'] as List)
              .map((e) => Map<String, dynamic>.from(e as Map))
              .toList();
        }
      }
    } catch (_) {
      try {
        final pkgRes = await ApiClient.instance.get('/sys/packages/upgrades');
        if (pkgRes['data'] is Map) {
          final d = pkgRes['data'] as Map<String, dynamic>;
          pkgUpgrades = d['count'] as int? ?? (d['packages'] is List ? (d['packages'] as List).length : 0);
          secUpgrades = d['security_count'] as int? ?? 0;
          if (d['packages'] is List) {
            pkgs = (d['packages'] as List)
                .map((e) => Map<String, dynamic>.from(e as Map))
                .toList();
          }
        }
      } catch (_) {}
    }

    final now = DateTime.now();
    final timeStr =
        '${now.hour.toString().padLeft(2, '0')}:${now.minute.toString().padLeft(2, '0')}:${now.second.toString().padLeft(2, '0')}';

    if (mounted) {
      setState(() {
        _serverCurrentVersion = currVer;
        _serverLatestVersion = needUpdate ? latVer : currVer;
        _serverNeedUpdate = needUpdate;
        _serverChangeLog = changeLog;
        _upgradablePackages = pkgUpgrades;
        _securityCount = secUpgrades;
        _packageList = pkgs;
        _lastServerCheckedTime = timeStr;
        _serverChecking = false;
      });
    }

    // Informational only - never feeds server-update decisions.
    try {
      final commitUri = Uri.parse(
          'https://api.github.com/repos/F-e-n-y-x/NivaroOS/commits/master');
      final cResp = await http.get(commitUri, headers: {
        'User-Agent': 'NivaroOS-Mobile'
      }).timeout(const Duration(seconds: 4));
      if (cResp.statusCode == 200) {
        final cBody = jsonDecode(cResp.body);
        if (cBody is Map<String, dynamic> && mounted) {
          setState(() => _latestCommit = cBody);
        }
      }
    } catch (_) {}
  }

  Future<void> _checkAppUpdate() async {
    setState(() => _appChecking = true);

    String installed = _appInstalledVersion;
    try {
      final info = await PackageInfo.fromPlatform();
      installed = 'v${info.version}';
    } catch (e) {
      debugPrint('[SystemUpdates] PackageInfo error: $e');
    }

    String latest = installed;
    String notes = '';
    String? apkUrl;
    int apkSize = 0;

    try {
      final releaseUri = Uri.parse(
          'https://api.github.com/repos/F-e-n-y-x/NivaroOS/releases/latest');
      final resp = await http.get(releaseUri, headers: {
        'User-Agent': 'NivaroOS-Mobile'
      }).timeout(const Duration(seconds: 4));
      if (resp.statusCode == 200) {
        final body = jsonDecode(resp.body);
        final tag = body['tag_name'] ?? body['name'];
        if (tag != null && tag.toString().isNotEmpty) {
          latest = tag.toString();
          if (!latest.startsWith('v')) latest = 'v$latest';
        }
        if (body['body'] != null && body['body'].toString().isNotEmpty) {
          notes = body['body'].toString();
        }
        if (body['assets'] is List) {
          for (final asset in body['assets']) {
            final name = asset['name']?.toString() ?? '';
            if (name.toLowerCase().endsWith('.apk')) {
              apkUrl = asset['browser_download_url']?.toString();
              apkSize = asset['size'] as int? ?? 0;
              break;
            }
          }
        }
      }
    } catch (e) {
      debugPrint('[SystemUpdates] GitHub release check error: $e');
    }

    if (mounted) {
      setState(() {
        _appInstalledVersion = installed;
        _appLatestVersion = latest;
        _appHasUpdate = _isNewer(latest, installed);
        _appReleaseNotes = notes;
        _appApkUrl = apkUrl;
        _appApkSizeBytes = apkSize;
        _appChecking = false;
      });
    }
  }

  Future<void> _startSystemUpdate() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(NivaroShape.large)),
        title: const Row(
          children: [
            Icon(Icons.system_security_update_rounded,
                color: NivaroColors.primaryLight, size: 24),
            SizedBox(width: 10),
            Text('Confirm Server Update',
                style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17)),
          ],
        ),
        content: Text(
          'Are you sure you want to trigger the NivaroOS server update pipeline to $_serverLatestVersion?\n\nCore services will apply latest components and restart automatically.',
          style: const TextStyle(
              color: NivaroColors.textSecondary, fontSize: 13.5, height: 1.4),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel',
                style: TextStyle(color: NivaroColors.textMuted)),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            style:
                FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
            child: const Text('Update Now',
                style: TextStyle(fontWeight: FontWeight.w700)),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    setState(() {
      _serverUpdating = true;
      _serverStatusMessage = 'Triggering NivaroOS server update pipeline...';
    });

    try {
      final res = await ApiClient.instance.post('/sys/update');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(res['message']?.toString() ??
                'Server update triggered successfully!'),
            backgroundColor: NivaroColors.success,
          ),
        );
        setState(() {
          _serverStatusMessage =
              'Server update triggered. Applying changes in background...';
        });
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Failed to trigger update: $e'),
            backgroundColor: NivaroColors.danger,
          ),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _serverUpdating = false);
        Future.delayed(const Duration(seconds: 4), () {
          if (mounted) _checkServerUpdate();
        });
      }
    }
  }

  Future<void> _refreshAptRepositories() async {
    setState(() => _refreshingRepos = true);
    try {
      final res = await ApiClient.instance.post('/sys/packages/refresh');
      if (res['data'] is Map && mounted) {
        final d = res['data'] as Map<String, dynamic>;
        final count = d['count'] as int? ?? 0;
        final secCount = d['security_count'] as int? ?? 0;
        List<Map<String, dynamic>> pkgs = [];
        if (d['packages'] is List) {
          pkgs = (d['packages'] as List)
              .map((e) => Map<String, dynamic>.from(e as Map))
              .toList();
        }
        setState(() {
          _upgradablePackages = count;
          _securityCount = secCount;
          _packageList = pkgs;
        });
      }
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('APT repositories refreshed. $_upgradablePackages packages upgradable.'),
            backgroundColor: NivaroColors.success,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to refresh repositories: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    } finally {
      if (mounted) setState(() => _refreshingRepos = false);
    }
  }

  Future<void> _upgradeLinuxPackages() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
        title: const Row(
          children: [
            Icon(Icons.inventory_2_rounded, color: NivaroColors.infoLight, size: 24),
            SizedBox(width: 10),
            Text('Upgrade System Packages', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17)),
          ],
        ),
        content: Text(
          'This will execute `apt-get dist-upgrade` on the host server to upgrade $_upgradablePackages packages${_securityCount > 0 ? " (including $_securityCount security updates)" : ""}.\n\nYou can watch live execution logs as packages are configured.',
          style: const TextStyle(color: NivaroColors.textSecondary, fontSize: 13.5, height: 1.4),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel', style: TextStyle(color: NivaroColors.textMuted)),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.info),
            child: const Text('Start Upgrade', style: TextStyle(fontWeight: FontWeight.w700)),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    setState(() => _upgradingApt = true);

    try {
      await ApiClient.instance.post('/sys/packages/upgrade');
      if (mounted) {
        _showUpgradeLogsModal();
      }
    } catch (e) {
      if (mounted) {
        setState(() => _upgradingApt = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to start package upgrade: $e'), backgroundColor: NivaroColors.danger),
        );
      }
    }
  }

  void _showUpgradeLogsModal() {
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (ctx) => _UpgradeLogsSheet(
        onFinished: () {
          if (mounted) {
            setState(() => _upgradingApt = false);
            _checkServerUpdate();
          }
        },
      ),
    ).then((_) {
      if (mounted) _checkServerUpdate();
    });
  }

  void _showPackagesListSheet() {
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      builder: (ctx) => _PackagesListSheet(
        packages: _packageList,
        securityCount: _securityCount,
        onUpgradeRequested: () {
          Navigator.of(ctx).pop();
          _upgradeLinuxPackages();
        },
      ),
    );
  }

  // Downloads the APK to a local temp file with progress, then hands it to
  // the OS package installer (OpenFilex) - the same download-then-open
  // pattern file_viewer_screen.dart already uses for any file, just pointed
  // at a public GitHub release asset instead of this server's own API (no
  // auth token needed - it's a public URL). Falls back to opening the
  // browser download if anything about the local flow fails, rather than
  // leaving the user stuck.
  Future<void> _downloadAndInstallApp() async {
    final url = _appApkUrl;
    if (url == null) {
      _openReleasePage();
      return;
    }
    setState(() {
      _appDownloading = true;
      _appDownloadProgress = 0.0;
    });
    try {
      final tempDir = await getTemporaryDirectory();
      final tempFile = File('${tempDir.path}/NivaroOS-$_appLatestVersion.apk');
      final client = http.Client();
      try {
        final req = http.Request('GET', Uri.parse(url));
        final streamedRes = await client.send(req);
        if (streamedRes.statusCode != 200) {
          throw Exception('Download failed (HTTP ${streamedRes.statusCode})');
        }
        final total = streamedRes.contentLength ?? _appApkSizeBytes;
        final sink = tempFile.openWrite();
        int received = 0;
        await for (final chunk in streamedRes.stream) {
          sink.add(chunk);
          received += chunk.length;
          if (mounted && total > 0) {
            setState(() =>
                _appDownloadProgress = (received / total).clamp(0.0, 1.0));
          }
        }
        await sink.flush();
        await sink.close();
      } finally {
        client.close();
      }
      if (mounted) setState(() => _appDownloading = false);
      await OpenFilex.open(tempFile.path);
    } catch (e) {
      if (mounted) {
        setState(() => _appDownloading = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
              content: Text('Download failed, opening browser instead: $e'),
              backgroundColor: NivaroColors.warning),
        );
      }
      _openReleasePage();
    }
  }

  Future<void> _openReleasePage() async {
    final urlStr =
        _appApkUrl ?? 'https://github.com/F-e-n-y-x/NivaroOS/releases/latest';
    try {
      await launchUrl(Uri.parse(urlStr), mode: LaunchMode.externalApplication);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not launch download URL: $e')),
        );
      }
    }
  }

  Future<void> _refreshAll() async {
    await Future.wait([_checkServerUpdate(), _checkAppUpdate()]);
  }

  @override
  Widget build(BuildContext context) {
    final checking = _serverChecking || _appChecking;
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('Updates Center',
            style: TextStyle(fontWeight: FontWeight.w800)),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh_rounded),
            tooltip: 'Refresh',
            onPressed: (checking || _serverUpdating) ? null : _refreshAll,
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _refreshAll,
        color: NivaroColors.primaryLight,
        backgroundColor: NivaroColors.surfaceRaised,
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 860),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 120),
              children: [
                // 1. Server Hero State Card
                _buildHeroCard(),
                const SizedBox(height: 18),

                // 2. Mobile App Update Card (fully independent track)
                _buildMobileAppCard(),
                const SizedBox(height: 18),

                // 3. Server Version Details
                _buildVersionInfoCard(),
                const SizedBox(height: 18),

                // 4. Linux Packages (APT) Status Card
                _buildLinuxPackagesCard(),
                const SizedBox(height: 18),

                // 5. Server Release Notes
                _buildHighlightsCard(),
                const SizedBox(height: 18),

                // 6. Master Branch Latest Commit (informational only)
                if (_latestCommit != null) ...[
                  _buildCommitCard(),
                  const SizedBox(height: 18),
                ],

                if (_serverStatusMessage != null)
                  Container(
                    padding: const EdgeInsets.all(12),
                    margin: const EdgeInsets.only(bottom: 16),
                    decoration: BoxDecoration(
                      color: NivaroColors.primary.withOpacity(0.12),
                      borderRadius: BorderRadius.circular(12),
                      border: Border.all(
                          color: NivaroColors.primaryLight.withOpacity(0.3)),
                    ),
                    child: Row(
                      children: [
                        const Icon(Icons.info_outline_rounded,
                            color: NivaroColors.primaryLight, size: 18),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Text(
                            _serverStatusMessage!,
                            style: const TextStyle(
                                color: NivaroColors.textPrimary,
                                fontSize: 12.5),
                          ),
                        ),
                      ],
                    ),
                  ),

                // Server Update Action Button
                if (_serverNeedUpdate)
                  FilledButton.icon(
                    onPressed: _serverUpdating ? null : _startSystemUpdate,
                    icon: _serverUpdating
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(
                                strokeWidth: 2, color: Colors.white))
                        : const Icon(Icons.system_security_update_rounded),
                    label: Text(
                      _serverUpdating
                          ? 'Installing Server Update...'
                          : 'Install Server Update ($_serverLatestVersion)',
                      style: const TextStyle(
                          fontWeight: FontWeight.w800, fontSize: 15),
                    ),
                    style: FilledButton.styleFrom(
                      backgroundColor: NivaroColors.primary,
                      minimumSize: const Size(double.infinity, 52),
                      shape: RoundedRectangleBorder(
                          borderRadius:
                              BorderRadius.circular(NivaroShape.large)),
                    ),
                  )
                else
                  FilledButton.icon(
                    onPressed: checking ? null : _refreshAll,
                    icon: checking
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(
                                strokeWidth: 2, color: Colors.white))
                        : const Icon(Icons.check_circle_outline_rounded),
                    label: Text(
                      checking
                          ? 'Checking for Updates...'
                          : 'Check for Updates',
                      style: const TextStyle(
                          fontWeight: FontWeight.w700, fontSize: 14.5),
                    ),
                    style: FilledButton.styleFrom(
                      backgroundColor: NivaroColors.surfaceRaised,
                      foregroundColor: NivaroColors.textPrimary,
                      side: const BorderSide(color: NivaroColors.borderSubtle),
                      minimumSize: const Size(double.infinity, 50),
                      shape: RoundedRectangleBorder(
                          borderRadius:
                              BorderRadius.circular(NivaroShape.large)),
                    ),
                  ),

                if (_lastServerCheckedTime.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  Center(
                    child: Text(
                      'Last checked today at $_lastServerCheckedTime',
                      style: const TextStyle(
                          color: NivaroColors.textMuted, fontSize: 11.5),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildHeroCard() {
    final isUpToDate = !_serverNeedUpdate;

    return DarkCard(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 24),
      child: Column(
        children: [
          Container(
            width: 74,
            height: 74,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: isUpToDate
                  ? NivaroColors.success.withOpacity(0.15)
                  : NivaroColors.warning.withOpacity(0.15),
              border: Border.all(
                color: isUpToDate
                    ? NivaroColors.successLight.withOpacity(0.5)
                    : NivaroColors.warning.withOpacity(0.5),
                width: 2,
              ),
              boxShadow: [
                BoxShadow(
                  color: isUpToDate
                      ? NivaroColors.success.withOpacity(0.2)
                      : NivaroColors.warning.withOpacity(0.2),
                  blurRadius: 16,
                  offset: const Offset(0, 4),
                ),
              ],
            ),
            alignment: Alignment.center,
            child: Icon(
              isUpToDate ? Icons.verified_rounded : Icons.new_releases_rounded,
              size: 38,
              color:
                  isUpToDate ? NivaroColors.successLight : NivaroColors.warning,
            ),
          ),
          const SizedBox(height: 16),
          Text(
            isUpToDate
                ? 'NivaroOS Server is Up to Date'
                : 'New Server Update Available',
            style: const TextStyle(
                fontWeight: FontWeight.w800,
                fontSize: 19,
                color: NivaroColors.textPrimary),
          ),
          const SizedBox(height: 6),
          Text(
            isUpToDate
                ? 'Your server is running the latest stable build ($_serverCurrentVersion)'
                : 'A new server release ($_serverLatestVersion) is available',
            textAlign: TextAlign.center,
            style: const TextStyle(
                color: NivaroColors.textSecondary, fontSize: 13),
          ),
        ],
      ),
    );
  }

  Widget _buildMobileAppCard() {
    final sizeMb = _appApkSizeBytes > 0
        ? ' (${(_appApkSizeBytes / (1024 * 1024)).toStringAsFixed(1)} MB)'
        : '';

    return DarkCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: (_appHasUpdate
                          ? NivaroColors.warning
                          : NivaroColors.primary)
                      .withOpacity(0.15),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(Icons.android_rounded,
                    color: _appHasUpdate
                        ? NivaroColors.warning
                        : NivaroColors.primaryLight,
                    size: 24),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('NivaroOS Mobile App',
                        style: TextStyle(
                            fontWeight: FontWeight.w800, fontSize: 15)),
                    const SizedBox(height: 2),
                    Text(
                      _appChecking
                          ? 'Checking...'
                          : (_appHasUpdate
                              ? 'Installed: $_appInstalledVersion · Latest: $_appLatestVersion$sizeMb'
                              : 'Installed: $_appInstalledVersion · Up to date'),
                      style: const TextStyle(
                          color: NivaroColors.textMuted, fontSize: 12),
                    ),
                  ],
                ),
              ),
              if (!_appChecking && !_appHasUpdate)
                const Icon(Icons.check_circle_rounded,
                    color: NivaroColors.successLight, size: 20),
            ],
          ),
          if (_appHasUpdate) ...[
            const SizedBox(height: 14),
            if (_appDownloading)
              Column(
                children: [
                  ClipRRect(
                    borderRadius: BorderRadius.circular(4),
                    child: LinearProgressIndicator(
                      value: _appDownloadProgress > 0
                          ? _appDownloadProgress
                          : null,
                      minHeight: 6,
                      backgroundColor: NivaroColors.surfaceRaised,
                      color: NivaroColors.primaryLight,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                      '${(_appDownloadProgress * 100).toStringAsFixed(0)}% downloaded',
                      style: const TextStyle(
                          color: NivaroColors.textMuted, fontSize: 11.5)),
                ],
              )
            else
              FilledButton.icon(
                onPressed: _downloadAndInstallApp,
                icon: const Icon(Icons.system_update_alt_rounded, size: 18),
                label: Text('Update App$sizeMb',
                    style: const TextStyle(
                        fontWeight: FontWeight.w700, fontSize: 13.5)),
                style: FilledButton.styleFrom(
                  backgroundColor: NivaroColors.primary,
                  minimumSize: const Size(double.infinity, 44),
                  shape: RoundedRectangleBorder(
                      borderRadius: BorderRadius.circular(10)),
                ),
              ),
            if (_appReleaseNotes.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(_appReleaseNotes,
                  style: const TextStyle(
                      color: NivaroColors.textSecondary,
                      fontSize: 12.5,
                      height: 1.4)),
            ],
          ],
        ],
      ),
    );
  }

  Widget _buildVersionInfoCard() {
    return DarkCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const SectionHeader(
            title: 'Server Version Information',
            subtitle: 'Release channel & system binaries',
          ),
          const SizedBox(height: 6),
          _infoRow('Installed Version', _serverCurrentVersion,
              isBadge: true, badgeColor: NivaroColors.primary),
          const Divider(height: 20, color: NivaroColors.borderSubtle),
          _infoRow('Latest Release', _serverLatestVersion,
              isBadge: true,
              badgeColor: _serverNeedUpdate
                  ? NivaroColors.warning
                  : NivaroColors.success),
          const Divider(height: 20, color: NivaroColors.borderSubtle),
          _infoRow('Release Channel', 'Stable (Production)', isBadge: false),
          const Divider(height: 20, color: NivaroColors.borderSubtle),
          _infoRow('Kernel Hypervisor', 'KVM Sidecar Active', isBadge: false),
          const Divider(height: 20, color: NivaroColors.borderSubtle),
          _infoRow('Host Architecture', 'x86_64 / Linux 6.x', isBadge: false),
        ],
      ),
    );
  }

  Widget _buildHighlightsCard() {
    return DarkCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.star_rounded,
                  color: NivaroColors.warning, size: 20),
              const SizedBox(width: 8),
              Text(
                _serverNeedUpdate
                    ? 'Server Release Notes ($_serverLatestVersion)'
                    : 'Server Release Highlights ($_serverCurrentVersion)',
                style: const TextStyle(
                    fontWeight: FontWeight.w700,
                    fontSize: 14.5,
                    color: NivaroColors.textPrimary),
              ),
            ],
          ),
          const SizedBox(height: 12),
          if (_serverChangeLog.isNotEmpty)
            Text(
              _serverChangeLog,
              style: const TextStyle(
                  color: NivaroColors.textSecondary, fontSize: 13, height: 1.5),
            )
          else
            const Text(
              '• Native Hypervisor KVM Sidecar for seamless Virtual Machine management\n• High-FPS Real-time Video Stream & VNC Remote Console\n• Native Terminal Emulator with VT100/ANSI PTY engine & xterm.dart\n• Zero-dependency Go Speedtest Engine (Internal & Local Link)\n• Unified Multi-Drive File Explorer with Companion Device Storage Sharing',
              style: TextStyle(
                  color: NivaroColors.textSecondary, fontSize: 13, height: 1.5),
            ),
        ],
      ),
    );
  }

  Widget _buildCommitCard() {
    final commit = _latestCommit!['commit'] as Map<String, dynamic>? ?? {};
    final sha = (_latestCommit!['sha']?.toString() ?? '').take(7);
    final rawMessage =
        commit['message']?.toString() ?? 'Latest repository commit';
    final message = rawMessage.split('\n').first;
    final author = commit['author']?['name']?.toString() ?? 'Nivaro Team';

    return DarkCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.code_rounded,
                  color: NivaroColors.primaryLight, size: 20),
              const SizedBox(width: 8),
              const Text('Latest Git Commit (master)',
                  style: TextStyle(fontWeight: FontWeight.w700, fontSize: 14)),
              const Spacer(),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
                decoration: BoxDecoration(
                  color: NivaroColors.surfaceRaised,
                  borderRadius: BorderRadius.circular(6),
                  border: Border.all(color: NivaroColors.borderSubtle),
                ),
                child: Text(
                  sha,
                  style: const TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.bold,
                      color: NivaroColors.primaryLight,
                      fontFamily: 'monospace'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(
            message,
            style: const TextStyle(
                fontWeight: FontWeight.w600,
                fontSize: 13,
                color: NivaroColors.textPrimary),
          ),
          const SizedBox(height: 4),
          Text(
            'Committed by $author',
            style:
                const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
          ),
        ],
      ),
    );
  }

  Widget _buildLinuxPackagesCard() {
    return DarkCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: (_securityCount > 0
                          ? NivaroColors.danger
                          : (_upgradablePackages > 0 ? NivaroColors.warning : NivaroColors.info))
                      .withOpacity(0.15),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(
                  _securityCount > 0
                      ? Icons.security_rounded
                      : Icons.inventory_2_rounded,
                  color: _securityCount > 0
                      ? NivaroColors.dangerLight
                      : (_upgradablePackages > 0 ? NivaroColors.warningLight : NivaroColors.infoLight),
                  size: 22,
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Linux Packages (APT)',
                        style: TextStyle(
                            fontWeight: FontWeight.w700, fontSize: 14.5)),
                    const SizedBox(height: 2),
                    Text(
                      _upgradablePackages > 0
                          ? '$_upgradablePackages package updates available${_securityCount > 0 ? " ($_securityCount security)" : ""}'
                          : 'Host OS repositories synced & up to date',
                      style: TextStyle(
                        color: _securityCount > 0
                            ? NivaroColors.dangerLight
                            : (_upgradablePackages > 0
                                ? NivaroColors.warning
                                : NivaroColors.textMuted),
                        fontSize: 12,
                        fontWeight: _upgradablePackages > 0 ? FontWeight.w600 : FontWeight.normal,
                      ),
                    ),
                  ],
                ),
              ),
              if (_upgradingApt)
                const SizedBox(
                  width: 20,
                  height: 20,
                  child: CircularProgressIndicator(strokeWidth: 2, color: NivaroColors.infoLight),
                )
              else
                Icon(
                  _upgradablePackages > 0
                      ? Icons.system_update_rounded
                      : Icons.check_circle_rounded,
                  color: _securityCount > 0
                      ? NivaroColors.dangerLight
                      : (_upgradablePackages > 0
                          ? NivaroColors.warning
                          : NivaroColors.successLight),
                  size: 20,
                ),
            ],
          ),
          const SizedBox(height: 14),
          // Actions Row: Refresh and View All
          Row(
            children: [
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: _refreshingRepos ? null : _refreshAptRepositories,
                  icon: _refreshingRepos
                      ? const SizedBox(
                          width: 14,
                          height: 14,
                          child: CircularProgressIndicator(strokeWidth: 1.5, color: NivaroColors.textPrimary))
                      : const Icon(Icons.sync_rounded, size: 16),
                  label: Text(_refreshingRepos ? 'Refreshing...' : 'Refresh Index',
                      style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600)),
                  style: OutlinedButton.styleFrom(
                    visualDensity: VisualDensity.compact,
                    foregroundColor: NivaroColors.textPrimary,
                    side: const BorderSide(color: NivaroColors.borderSubtle),
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                  ),
                ),
              ),
              if (_packageList.isNotEmpty) ...[
                const SizedBox(width: 8),
                Expanded(
                  child: OutlinedButton.icon(
                    onPressed: _showPackagesListSheet,
                    icon: const Icon(Icons.list_alt_rounded, size: 16),
                    label: Text('View (${_packageList.length})',
                        style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600)),
                    style: OutlinedButton.styleFrom(
                      visualDensity: VisualDensity.compact,
                      foregroundColor: NivaroColors.primaryLight,
                      side: const BorderSide(color: NivaroColors.borderHighlight),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
                    ),
                  ),
                ),
              ],
            ],
          ),
          if (_upgradingApt) ...[
            const SizedBox(height: 10),
            FilledButton.icon(
              onPressed: _showUpgradeLogsModal,
              icon: const Icon(Icons.terminal_rounded, size: 18),
              label: const Text('Upgrade in Progress · View Live Logs',
                  style: TextStyle(fontWeight: FontWeight.w700, fontSize: 13)),
              style: FilledButton.styleFrom(
                backgroundColor: NivaroColors.info,
                minimumSize: const Size(double.infinity, 42),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
              ),
            ),
          ] else if (_upgradablePackages > 0) ...[
            const SizedBox(height: 10),
            FilledButton.icon(
              onPressed: _serverUpdating ? null : _upgradeLinuxPackages,
              icon: const Icon(Icons.upgrade_rounded, size: 18),
              label: Text('Upgrade All $_upgradablePackages Packages',
                  style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13)),
              style: FilledButton.styleFrom(
                backgroundColor: _securityCount > 0 ? NivaroColors.danger : NivaroColors.primary,
                minimumSize: const Size(double.infinity, 42),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
              ),
            ),
          ],
        ],
      ),
    );
  }

  Widget _infoRow(String label, String value,
      {bool isBadge = false, Color? badgeColor}) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(label,
            style: const TextStyle(
                color: NivaroColors.textSecondary, fontSize: 13)),
        if (isBadge)
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
            decoration: BoxDecoration(
              color: (badgeColor ?? NivaroColors.primary).withOpacity(0.18),
              borderRadius: BorderRadius.circular(12),
              border: Border.all(
                  color: (badgeColor ?? NivaroColors.primary).withOpacity(0.4)),
            ),
            child: Text(
              value,
              style: TextStyle(
                color: badgeColor ?? NivaroColors.primaryLight,
                fontWeight: FontWeight.w700,
                fontSize: 12,
              ),
            ),
          )
        else
          Text(
            value,
            style: const TextStyle(
                fontWeight: FontWeight.w600,
                fontSize: 13,
                color: NivaroColors.textPrimary),
          ),
      ],
    );
  }
}

class _UpgradeLogsSheet extends StatefulWidget {
  final VoidCallback onFinished;
  const _UpgradeLogsSheet({required this.onFinished});

  @override
  State<_UpgradeLogsSheet> createState() => _UpgradeLogsSheetState();
}

class _UpgradeLogsSheetState extends State<_UpgradeLogsSheet> {
  Timer? _timer;
  List<String> _logs = [];
  bool _isRunning = true;
  int _exitCode = 0;
  final ScrollController _scrollController = ScrollController();

  @override
  void initState() {
    super.initState();
    _pollStatus();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) => _pollStatus());
  }

  @override
  void dispose() {
    _timer?.cancel();
    _scrollController.dispose();
    super.dispose();
  }

  Future<void> _pollStatus() async {
    try {
      final res = await ApiClient.instance.get('/sys/packages/upgrade/status');
      if (res['data'] is Map && mounted) {
        final d = res['data'] as Map<String, dynamic>;
        final running = d['running'] == true;
        final rawLogs = d['logs'] as List<dynamic>? ?? [];
        final logs = rawLogs.map((e) => e.toString()).toList();
        final exitCode = d['exit_code'] as int? ?? 0;

        setState(() {
          _isRunning = running;
          _logs = logs;
          _exitCode = exitCode;
        });

        if (!running) {
          _timer?.cancel();
          widget.onFinished();
        }

        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (_scrollController.hasClients) {
            _scrollController.jumpTo(_scrollController.position.maxScrollExtent);
          }
        });
      }
    } catch (_) {}
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      height: MediaQuery.of(context).size.height * 0.75,
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      child: Column(
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
            decoration: const BoxDecoration(
              border: Border(bottom: BorderSide(color: NivaroColors.borderSubtle)),
            ),
            child: Row(
              children: [
                if (_isRunning)
                  const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2, color: NivaroColors.infoLight),
                  )
                else
                  Icon(
                    _exitCode == 0 ? Icons.check_circle_rounded : Icons.error_rounded,
                    color: _exitCode == 0 ? NivaroColors.successLight : NivaroColors.dangerLight,
                    size: 20,
                  ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _isRunning
                            ? 'Upgrading System Packages...'
                            : (_exitCode == 0 ? 'Package Upgrade Finished' : 'Upgrade Finished With Code $_exitCode'),
                        style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 15),
                      ),
                      Text(
                        _isRunning ? 'Running apt-get dist-upgrade in background' : '${_logs.length} log lines recorded',
                        style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
                      ),
                    ],
                  ),
                ),
                IconButton(
                  icon: const Icon(Icons.close_rounded),
                  onPressed: () => Navigator.of(context).pop(),
                ),
              ],
            ),
          ),
          Expanded(
            child: Container(
              color: const Color(0xFF0D1117),
              padding: const EdgeInsets.all(14),
              child: _logs.isEmpty
                  ? const Center(
                      child: Text('Waiting for output from apt...',
                          style: TextStyle(color: NivaroColors.textMuted, fontFamily: 'monospace')),
                    )
                  : ListView.builder(
                      controller: _scrollController,
                      itemCount: _logs.length,
                      itemBuilder: (context, i) {
                        final line = _logs[i];
                        Color color = const Color(0xFFC9D1D9);
                        if (line.toLowerCase().contains('error') || line.toLowerCase().contains('failed')) {
                          color = NivaroColors.dangerLight;
                        } else if (line.toLowerCase().contains('setting up') ||
                            line.toLowerCase().contains('unpacking')) {
                          color = NivaroColors.infoLight;
                        } else if (line.toLowerCase().contains('success') ||
                            line.toLowerCase().contains('completed')) {
                          color = NivaroColors.successLight;
                        }
                        return Text(
                          line,
                          style: TextStyle(
                            fontFamily: 'monospace',
                            fontSize: 11.5,
                            height: 1.35,
                            color: color,
                          ),
                        );
                      },
                    ),
            ),
          ),
        ],
      ),
    );
  }
}

class _PackagesListSheet extends StatefulWidget {
  final List<Map<String, dynamic>> packages;
  final int securityCount;
  final VoidCallback onUpgradeRequested;

  const _PackagesListSheet({
    required this.packages,
    required this.securityCount,
    required this.onUpgradeRequested,
  });

  @override
  State<_PackagesListSheet> createState() => _PackagesListSheetState();
}

class _PackagesListSheetState extends State<_PackagesListSheet> {
  String _filter = '';

  @override
  Widget build(BuildContext context) {
    final filtered = widget.packages.where((p) {
      final name = p['name']?.toString().toLowerCase() ?? '';
      return _filter.isEmpty || name.contains(_filter.toLowerCase());
    }).toList();

    return Container(
      height: MediaQuery.of(context).size.height * 0.85,
      decoration: const BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
      ),
      child: Column(
        children: [
          Container(
            padding: const EdgeInsets.fromLTRB(20, 16, 12, 12),
            decoration: const BoxDecoration(
              border: Border(bottom: BorderSide(color: NivaroColors.borderSubtle)),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Center(
                  child: Container(
                    width: 38,
                    height: 4,
                    margin: const EdgeInsets.only(bottom: 14),
                    decoration: BoxDecoration(
                      color: NivaroColors.borderHighlight,
                      borderRadius: BorderRadius.circular(2),
                    ),
                  ),
                ),
                Row(
                  children: [
                    const Icon(Icons.inventory_2_rounded, color: NivaroColors.infoLight, size: 22),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        'Upgradable APT Packages (${widget.packages.length})',
                        style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 17),
                      ),
                    ),
                    if (widget.securityCount > 0)
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                        margin: const EdgeInsets.only(right: 8),
                        decoration: BoxDecoration(
                          color: NivaroColors.danger.withOpacity(0.2),
                          borderRadius: BorderRadius.circular(8),
                          border: Border.all(color: NivaroColors.dangerLight.withOpacity(0.4)),
                        ),
                        child: Text(
                          '${widget.securityCount} Security',
                          style: const TextStyle(
                              color: NivaroColors.dangerLight, fontSize: 11, fontWeight: FontWeight.bold),
                        ),
                      ),
                    IconButton(
                      icon: const Icon(Icons.close_rounded),
                      onPressed: () => Navigator.of(context).pop(),
                    ),
                  ],
                ),
                const SizedBox(height: 12),
                TextField(
                  onChanged: (val) => setState(() => _filter = val),
                  decoration: InputDecoration(
                    hintText: 'Search packages...',
                    hintStyle: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                    prefixIcon: const Icon(Icons.search_rounded, size: 20, color: NivaroColors.textMuted),
                    filled: true,
                    fillColor: NivaroColors.surfaceRaised,
                    contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                    border: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(12),
                      borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                    ),
                    enabledBorder: OutlineInputBorder(
                      borderRadius: BorderRadius.circular(12),
                      borderSide: const BorderSide(color: NivaroColors.borderSubtle),
                    ),
                  ),
                ),
              ],
            ),
          ),
          Expanded(
            child: filtered.isEmpty
                ? const Center(
                    child: Text('No matching packages found.', style: TextStyle(color: NivaroColors.textMuted)),
                  )
                : ListView.separated(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    itemCount: filtered.length,
                    separatorBuilder: (_, __) => const Divider(height: 1, color: NivaroColors.borderSubtle),
                    itemBuilder: (context, i) {
                      final p = filtered[i];
                      final isSec = p['is_security'] == true;
                      return ListTile(
                        dense: true,
                        contentPadding: const EdgeInsets.symmetric(horizontal: 20, vertical: 2),
                        title: Row(
                          children: [
                            Expanded(
                              child: Text(
                                p['name']?.toString() ?? '',
                                style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 13.5),
                              ),
                            ),
                            if (isSec)
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                                decoration: BoxDecoration(
                                  color: NivaroColors.danger.withOpacity(0.18),
                                  borderRadius: BorderRadius.circular(6),
                                ),
                                child: const Row(
                                  mainAxisSize: MainAxisSize.min,
                                  children: [
                                    Icon(Icons.security_rounded, color: NivaroColors.dangerLight, size: 12),
                                    SizedBox(width: 4),
                                    Text(
                                      'Security',
                                      style: TextStyle(
                                          color: NivaroColors.dangerLight,
                                          fontSize: 10.5,
                                          fontWeight: FontWeight.bold),
                                    ),
                                  ],
                                ),
                              ),
                          ],
                        ),
                        subtitle: Padding(
                          padding: const EdgeInsets.only(top: 3),
                          child: Row(
                            children: [
                              Text(
                                p['current_version']?.toString() ?? '',
                                style: const TextStyle(
                                    color: NivaroColors.textMuted, fontSize: 11.5, fontFamily: 'monospace'),
                              ),
                              const SizedBox(width: 6),
                              const Icon(Icons.arrow_forward_rounded, size: 12, color: NivaroColors.primaryLight),
                              const SizedBox(width: 6),
                              Text(
                                p['new_version']?.toString() ?? '',
                                style: const TextStyle(
                                    color: NivaroColors.successLight,
                                    fontSize: 11.5,
                                    fontWeight: FontWeight.w600,
                                    fontFamily: 'monospace'),
                              ),
                              const Spacer(),
                              Text(
                                p['arch']?.toString() ?? '',
                                style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
                              ),
                            ],
                          ),
                        ),
                      );
                    },
                  ),
          ),
          Container(
            padding: const EdgeInsets.all(16),
            decoration: const BoxDecoration(
              border: Border(top: BorderSide(color: NivaroColors.borderSubtle)),
            ),
            child: FilledButton.icon(
              onPressed: widget.onUpgradeRequested,
              icon: const Icon(Icons.upgrade_rounded),
              label: Text('Upgrade All (${widget.packages.length} Packages)'),
              style: FilledButton.styleFrom(
                backgroundColor: NivaroColors.primary,
                minimumSize: const Size(double.infinity, 48),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

extension StringExt on String {
  String take(int n) => length <= n ? this : substring(0, n);
}
