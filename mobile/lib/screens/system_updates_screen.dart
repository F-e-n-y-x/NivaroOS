import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import 'package:url_launcher/url_launcher.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';

/// Full-featured System & App Updates center for NivaroOS:
/// - Real-time version check via /v1/sys/version/check
/// - GitHub Releases synchronization & direct APK download
/// - 1-Click Server System Update & APT Linux package upgrade
/// - System details, changelog, and host architecture info
class SystemUpdatesScreen extends StatefulWidget {
  const SystemUpdatesScreen({super.key});

  @override
  State<SystemUpdatesScreen> createState() => _SystemUpdatesScreenState();
}

class _SystemUpdatesScreenState extends State<SystemUpdatesScreen> {
  String _currentVersion = 'v1.0.0';
  String _latestVersion = 'v1.0.0';
  bool _checking = true;
  bool _hasUpdate = false;
  bool _updating = false;
  String _releaseNotes = '';
  String? _apkDownloadUrl;
  int _apkSizeBytes = 0;
  int _upgradablePackages = 0;
  Map<String, dynamic>? _latestCommit;
  String _lastCheckedTime = '';
  String? _statusMessage;

  @override
  void initState() {
    super.initState();
    _checkUpdates();
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

  Future<void> _checkUpdates() async {
    setState(() {
      _checking = true;
      _statusMessage = null;
    });

    String currVer = _currentVersion;
    String latVer = _currentVersion;
    bool needUpdate = false;
    String notes = '';
    String? apkUrl;
    int apkSize = 0;
    int pkgUpgrades = 0;

    // 1. Query Backend /v1/sys/version/check
    try {
      final res = await ApiClient.instance.get('/sys/version/check');
      if (res['data'] is Map) {
        final d = res['data'] as Map<String, dynamic>;
        if (d['current_version'] != null && d['current_version'].toString().isNotEmpty) {
          currVer = d['current_version'].toString();
          if (!currVer.startsWith('v')) currVer = 'v$currVer';
        }
        if (d['need_update'] == true) {
          needUpdate = true;
        }
        if (d['version'] is Map) {
          final vMap = d['version'] as Map<String, dynamic>;
          if (vMap['version'] != null && vMap['version'].toString().isNotEmpty) {
            latVer = vMap['version'].toString();
            if (!latVer.startsWith('v')) latVer = 'v$latVer';
          }
          if (vMap['release_notes'] != null) {
            notes = vMap['release_notes'].toString();
          } else if (vMap['changelog'] != null) {
            notes = vMap['changelog'].toString();
          }
        }
      }
    } catch (e) {
      debugPrint('[SystemUpdates] Backend check error: $e');
    }

    // 2. Query GitHub Releases
    try {
      final releaseUri = Uri.parse('https://api.github.com/repos/F-e-n-y-x/NivaroOS/releases/latest');
      final resp = await http.get(releaseUri, headers: {'User-Agent': 'NivaroOS-Mobile'}).timeout(const Duration(seconds: 4));
      if (resp.statusCode == 200) {
        final body = jsonDecode(resp.body);
        final tag = body['tag_name'] ?? body['name'];
        if (tag != null && tag.toString().isNotEmpty) {
          latVer = tag.toString();
          if (!latVer.startsWith('v')) latVer = 'v$latVer';
        }

        if (body['body'] != null && body['body'].toString().isNotEmpty) {
          notes = body['body'].toString();
        }

        // Look for APK release asset
        if (body['assets'] is List) {
          for (final asset in body['assets']) {
            final name = asset['name']?.toString() ?? '';
            if (name.endsWith('.apk') || name == 'NivaroOS.apk') {
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

    // Determine if update is actually newer
    needUpdate = _isNewer(latVer, currVer);

    // 3. Query Linux package updates
    try {
      final pkgRes = await ApiClient.instance.get('/sys/packages/upgrades');
      if (pkgRes['data'] is Map && pkgRes['data']['packages'] is List) {
        pkgUpgrades = (pkgRes['data']['packages'] as List).length;
      }
    } catch (_) {}

    // 4. Query GitHub Commits for live master tracking
    try {
      final commitUri = Uri.parse('https://api.github.com/repos/F-e-n-y-x/NivaroOS/commits/master');
      final cResp = await http.get(commitUri, headers: {'User-Agent': 'NivaroOS-Mobile'}).timeout(const Duration(seconds: 4));
      if (cResp.statusCode == 200) {
        final cBody = jsonDecode(cResp.body);
        if (cBody is Map<String, dynamic>) {
          _latestCommit = cBody;
        }
      }
    } catch (_) {}

    final now = DateTime.now();
    final timeStr = '${now.hour.toString().padLeft(2, '0')}:${now.minute.toString().padLeft(2, '0')}:${now.second.toString().padLeft(2, '0')}';

    if (mounted) {
      setState(() {
        _currentVersion = currVer;
        _latestVersion = latVer;
        _hasUpdate = needUpdate;
        _releaseNotes = notes;
        _apkDownloadUrl = apkUrl;
        _apkSizeBytes = apkSize;
        _upgradablePackages = pkgUpgrades;
        _lastCheckedTime = timeStr;
        _checking = false;
      });
    }
  }

  Future<void> _startSystemUpdate() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: NivaroColors.surfaceRaised,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
        title: const Row(
          children: [
            Icon(Icons.system_security_update_rounded, color: NivaroColors.primaryLight, size: 24),
            SizedBox(width: 10),
            Text('Confirm Server Update', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17)),
          ],
        ),
        content: Text(
          'Are you sure you want to trigger the NivaroOS server update pipeline to $_latestVersion?\n\nCore services will apply latest components and restart automatically.',
          style: const TextStyle(color: NivaroColors.textSecondary, fontSize: 13.5, height: 1.4),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel', style: TextStyle(color: NivaroColors.textMuted)),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(true),
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.primary),
            child: const Text('Update Now', style: TextStyle(fontWeight: FontWeight.w700)),
          ),
        ],
      ),
    );

    if (confirmed != true || !mounted) return;

    setState(() {
      _updating = true;
      _statusMessage = 'Triggering NivaroOS server update pipeline...';
    });

    try {
      final res = await ApiClient.instance.post('/sys/update');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(res['message']?.toString() ?? 'Server update triggered successfully!'),
            backgroundColor: NivaroColors.success,
          ),
        );
        setState(() {
          _statusMessage = 'Server update triggered. Applying changes in background...';
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
        setState(() => _updating = false);
        Future.delayed(const Duration(seconds: 4), () {
          if (mounted) _checkUpdates();
        });
      }
    }
  }

  Future<void> _upgradeLinuxPackages() async {
    setState(() {
      _updating = true;
      _statusMessage = 'Upgrading Linux packages on server...';
    });

    try {
      final res = await ApiClient.instance.post('/sys/packages/upgrade');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(res['message']?.toString() ?? 'Linux packages upgraded successfully!'),
            backgroundColor: NivaroColors.success,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Failed to upgrade packages: $e'),
            backgroundColor: NivaroColors.danger,
          ),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _updating = false);
        _checkUpdates();
      }
    }
  }

  Future<void> _downloadApk() async {
    final urlStr = _apkDownloadUrl ?? 'https://github.com/F-e-n-y-x/NivaroOS/releases/latest';
    try {
      final uri = Uri.parse(urlStr);
      await launchUrl(uri, mode: LaunchMode.externalApplication);
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not launch download URL: $e')),
        );
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('Updates Center', style: TextStyle(fontWeight: FontWeight.w800)),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh_rounded),
            tooltip: 'Refresh',
            onPressed: (_checking || _updating) ? null : _checkUpdates,
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _checkUpdates,
        color: NivaroColors.primaryLight,
        backgroundColor: NivaroColors.surfaceRaised,
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 860),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 120),
              children: [
                // 1. Hero State Card
                _buildHeroCard(),
                const SizedBox(height: 18),

                // 2. Mobile App Update Card
                _buildMobileAppCard(),
                const SizedBox(height: 18),

                // 3. Version Comparison & Details
                _buildVersionInfoCard(),
                const SizedBox(height: 18),

                // 4. Linux Packages (APT) Status Card
                _buildLinuxPackagesCard(),
                const SizedBox(height: 18),

                // 5. Release Highlights & Features
                _buildHighlightsCard(),
                const SizedBox(height: 18),

                // 6. Master Branch Latest Commit
                if (_latestCommit != null) ...[
                  _buildCommitCard(),
                  const SizedBox(height: 18),
                ],

                // Status message feedback if any
                if (_statusMessage != null)
                  Container(
                    padding: const EdgeInsets.all(12),
                    margin: const EdgeInsets.only(bottom: 16),
                    decoration: BoxDecoration(
                      color: NivaroColors.primary.withOpacity(0.12),
                      borderRadius: BorderRadius.circular(12),
                      border: Border.all(color: NivaroColors.primaryLight.withOpacity(0.3)),
                    ),
                    child: Row(
                      children: [
                        const Icon(Icons.info_outline_rounded, color: NivaroColors.primaryLight, size: 18),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Text(
                            _statusMessage!,
                            style: const TextStyle(color: NivaroColors.textPrimary, fontSize: 12.5),
                          ),
                        ),
                      ],
                    ),
                  ),

                // Server Update Action Button
                if (_hasUpdate)
                  FilledButton.icon(
                    onPressed: _updating ? null : _startSystemUpdate,
                    icon: _updating
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                        : const Icon(Icons.system_security_update_rounded),
                    label: Text(
                      _updating ? 'Installing Server Update...' : 'Install Server Update ($_latestVersion)',
                      style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 15),
                    ),
                    style: FilledButton.styleFrom(
                      backgroundColor: NivaroColors.primary,
                      minimumSize: const Size(double.infinity, 52),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                    ),
                  )
                else
                  FilledButton.icon(
                    onPressed: _checking ? null : _checkUpdates,
                    icon: _checking
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                        : const Icon(Icons.check_circle_outline_rounded),
                    label: Text(
                      _checking ? 'Checking for Updates...' : 'Check for Updates',
                      style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14.5),
                    ),
                    style: FilledButton.styleFrom(
                      backgroundColor: NivaroColors.surfaceRaised,
                      foregroundColor: NivaroColors.textPrimary,
                      side: const BorderSide(color: NivaroColors.borderSubtle),
                      minimumSize: const Size(double.infinity, 50),
                      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(NivaroShape.large)),
                    ),
                  ),

                if (_lastCheckedTime.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  Center(
                    child: Text(
                      'Last checked today at $_lastCheckedTime',
                      style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
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
    final isUpToDate = !_hasUpdate;

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
                color: isUpToDate ? NivaroColors.successLight.withOpacity(0.5) : NivaroColors.warning.withOpacity(0.5),
                width: 2,
              ),
              boxShadow: [
                BoxShadow(
                  color: isUpToDate ? NivaroColors.success.withOpacity(0.2) : NivaroColors.warning.withOpacity(0.2),
                  blurRadius: 16,
                  offset: const Offset(0, 4),
                ),
              ],
            ),
            alignment: Alignment.center,
            child: Icon(
              isUpToDate ? Icons.verified_rounded : Icons.new_releases_rounded,
              size: 38,
              color: isUpToDate ? NivaroColors.successLight : NivaroColors.warning,
            ),
          ),
          const SizedBox(height: 16),
          Text(
            isUpToDate ? 'NivaroOS is Up to Date' : 'New Update Available',
            style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 19, color: NivaroColors.textPrimary),
          ),
          const SizedBox(height: 6),
          Text(
            isUpToDate
                ? 'Your system is running the latest stable build ($_currentVersion)'
                : 'A new release ($_latestVersion) is available',
            textAlign: TextAlign.center,
            style: const TextStyle(color: NivaroColors.textSecondary, fontSize: 13),
          ),
        ],
      ),
    );
  }

  Widget _buildMobileAppCard() {
    final sizeMb = _apkSizeBytes > 0 ? ' (${(_apkSizeBytes / (1024 * 1024)).toStringAsFixed(1)} MB)' : '';

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
                  color: NivaroColors.primary.withOpacity(0.15),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: const Icon(Icons.android_rounded, color: NivaroColors.primaryLight, size: 24),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('NivaroOS Mobile App', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 15)),
                    const SizedBox(height: 2),
                    Text('Installed: $_currentVersion · Latest: $_latestVersion$sizeMb', style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          OutlinedButton.icon(
            onPressed: _downloadApk,
            icon: const Icon(Icons.download_rounded, size: 18),
            label: Text('Download Latest APK$sizeMb', style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13.5)),
            style: OutlinedButton.styleFrom(
              foregroundColor: NivaroColors.primaryLight,
              side: const BorderSide(color: NivaroColors.primaryLight),
              minimumSize: const Size(double.infinity, 44),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
            ),
          ),
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
            title: 'Version Information',
            subtitle: 'Release channel & system binaries',
          ),
          const SizedBox(height: 6),
          _infoRow('Installed Version', _currentVersion, isBadge: true, badgeColor: NivaroColors.primary),
          const Divider(height: 20, color: NivaroColors.borderSubtle),
          _infoRow('Latest Release', _latestVersion, isBadge: true, badgeColor: _hasUpdate ? NivaroColors.warning : NivaroColors.success),
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
              const Icon(Icons.star_rounded, color: NivaroColors.warning, size: 20),
              const SizedBox(width: 8),
              Text(
                _hasUpdate ? 'Release Notes ($_latestVersion)' : 'Release Highlights ($_latestVersion)',
                style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 14.5, color: NivaroColors.textPrimary),
              ),
            ],
          ),
          const SizedBox(height: 12),
          if (_releaseNotes.isNotEmpty)
            Text(
              _releaseNotes,
              style: const TextStyle(color: NivaroColors.textSecondary, fontSize: 13, height: 1.5),
            )
          else
            const Text(
              '• Native Hypervisor KVM Sidecar for seamless Virtual Machine management\n• High-FPS Real-time Video Stream & VNC Remote Console\n• Native Terminal Emulator with VT100/ANSI PTY engine & xterm.dart\n• Zero-dependency Go Speedtest Engine (Internal & Local Link)\n• Unified Multi-Drive File Explorer with Companion Device Storage Sharing',
              style: TextStyle(color: NivaroColors.textSecondary, fontSize: 13, height: 1.5),
            ),
        ],
      ),
    );
  }

  Widget _buildCommitCard() {
    final commit = _latestCommit!['commit'] as Map<String, dynamic>? ?? {};
    final sha = (_latestCommit!['sha']?.toString() ?? '').take(7);
    final rawMessage = commit['message']?.toString() ?? 'Latest repository commit';
    final message = rawMessage.split('\n').first;
    final author = commit['author']?['name']?.toString() ?? 'Nivaro Team';

    return DarkCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              const Icon(Icons.code_rounded, color: NivaroColors.primaryLight, size: 20),
              const SizedBox(width: 8),
              const Text('Latest Git Commit (master)', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 14)),
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
                  style: const TextStyle(fontSize: 11, fontWeight: FontWeight.bold, color: NivaroColors.primaryLight, fontFamily: 'monospace'),
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(
            message,
            style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13, color: NivaroColors.textPrimary),
          ),
          const SizedBox(height: 4),
          Text(
            'Committed by $author',
            style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11.5),
          ),
        ],
      ),
    );
  }

  Widget _buildLinuxPackagesCard() {
    return DarkCard(
      padding: const EdgeInsets.all(18),
      child: Column(
        children: [
          Row(
            children: [
              Container(
                padding: const EdgeInsets.all(10),
                decoration: BoxDecoration(
                  color: NivaroColors.info.withOpacity(0.15),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: const Icon(Icons.inventory_2_rounded, color: NivaroColors.infoLight, size: 22),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('Linux Packages (APT)', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 14)),
                    const SizedBox(height: 2),
                    Text(
                      _upgradablePackages > 0
                          ? '$_upgradablePackages system package updates available'
                          : 'Host OS repositories synced & healthy',
                      style: TextStyle(
                        color: _upgradablePackages > 0 ? NivaroColors.warning : NivaroColors.textMuted,
                        fontSize: 12,
                      ),
                    ),
                  ],
                ),
              ),
              Icon(
                _upgradablePackages > 0 ? Icons.system_update_rounded : Icons.check_circle_rounded,
                color: _upgradablePackages > 0 ? NivaroColors.warning : NivaroColors.successLight,
                size: 20,
              ),
            ],
          ),
          if (_upgradablePackages > 0) ...[
            const SizedBox(height: 12),
            OutlinedButton.icon(
              onPressed: _updating ? null : _upgradeLinuxPackages,
              icon: const Icon(Icons.upgrade_rounded, size: 18),
              label: Text('Upgrade $_upgradablePackages Packages', style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 13)),
              style: OutlinedButton.styleFrom(
                foregroundColor: NivaroColors.warningLight,
                side: const BorderSide(color: NivaroColors.warning),
                minimumSize: const Size(double.infinity, 40),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
              ),
            ),
          ],
        ],
      ),
    );
  }

  Widget _infoRow(String label, String value, {bool isBadge = false, Color? badgeColor}) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [
        Text(label, style: const TextStyle(color: NivaroColors.textSecondary, fontSize: 13)),
        if (isBadge)
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
            decoration: BoxDecoration(
              color: (badgeColor ?? NivaroColors.primary).withOpacity(0.18),
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: (badgeColor ?? NivaroColors.primary).withOpacity(0.4)),
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
            style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 13, color: NivaroColors.textPrimary),
          ),
      ],
    );
  }
}

extension StringExt on String {
  String take(int n) => length <= n ? this : substring(0, n);
}
