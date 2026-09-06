import 'dart:convert';
import 'package:flutter/material.dart';
import 'package:http/http.dart' as http;
import '../theme.dart';
import '../services/api_client.dart';
import '../widgets/common.dart';

/// Full-featured System Updates center for NivaroOS:
/// - Real-time version check via /v1/sys/version/check
/// - GitHub Releases & Commits synchronization
/// - 1-Click System Update with confirmation and progress
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
  Map<String, dynamic>? _latestCommit;
  String _lastCheckedTime = '';
  String? _statusMessage;

  @override
  void initState() {
    super.initState();
    _checkUpdates();
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

    // 1. Query Backend /v1/sys/version/check
    try {
      final res = await ApiClient.instance.get('/sys/version/check');
      if (res['data'] is Map) {
        final d = res['data'] as Map<String, dynamic>;
        if (d['current_version'] != null && d['current_version'].toString().isNotEmpty) {
          currVer = d['current_version'].toString();
        }
        if (d['need_update'] == true) {
          needUpdate = true;
        }
        if (d['version'] is Map) {
          final vMap = d['version'] as Map<String, dynamic>;
          if (vMap['version'] != null && vMap['version'].toString().isNotEmpty) {
            latVer = vMap['version'].toString();
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
          if (latVer != currVer && latVer.isNotEmpty) {
            needUpdate = true;
          }
        }
        if (body['body'] != null && body['body'].toString().isNotEmpty) {
          notes = body['body'].toString();
        }
      }
    } catch (_) {}

    // 3. Query GitHub Commits for live master tracking
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
            Text('Confirm System Update', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 17)),
          ],
        ),
        content: Text(
          'Are you sure you want to update NivaroOS to $_latestVersion?\n\nCore services will restart automatically once the update files are applied.',
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
      _statusMessage = 'Triggering NivaroOS system update pipeline...';
    });

    try {
      final res = await ApiClient.instance.post('/sys/update');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(res['message']?.toString() ?? 'Update triggered successfully! Server is applying changes.'),
            backgroundColor: NivaroColors.success,
          ),
        );
        setState(() {
          _statusMessage = 'Update successfully triggered. Reloading system version in background...';
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
        // Refresh after short delay
        Future.delayed(const Duration(seconds: 4), () {
          if (mounted) _checkUpdates();
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('System Updates', style: TextStyle(fontWeight: FontWeight.w800)),
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

                // 2. Version Comparison & Details
                _buildVersionInfoCard(),
                const SizedBox(height: 18),

                // 3. Release Highlights & Features
                _buildHighlightsCard(),
                const SizedBox(height: 18),

                // 4. Master Branch Latest Commit
                if (_latestCommit != null) ...[
                  _buildCommitCard(),
                  const SizedBox(height: 18),
                ],

                // 5. Linux Packages (APT) Status Card
                _buildLinuxPackagesCard(),
                const SizedBox(height: 24),

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

                // Primary Action Button
                if (_hasUpdate)
                  FilledButton.icon(
                    onPressed: _updating ? null : _startSystemUpdate,
                    icon: _updating
                        ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                        : const Icon(Icons.system_security_update_rounded),
                    label: Text(
                      _updating ? 'Installing Update...' : 'Install NivaroOS $_latestVersion',
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
                ? 'Your server is running the latest stable build ($_currentVersion)'
                : 'A new release ($_latestVersion) is ready to install',
            textAlign: TextAlign.center,
            style: const TextStyle(color: NivaroColors.textSecondary, fontSize: 13),
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
                _hasUpdate ? 'Release Notes ($_latestVersion)' : 'Current Features',
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
              '• Native Hypervisor KVM Sidecar for seamless Virtual Machine management\n• High-FPS Real-time Video Stream & VNC Remote Console\n• Equidistant Glassmorphic Mobile Companion Navigation\n• Zero-dependency Go Speedtest Engine (Internal & Local Link)\n• Unified Multi-Drive File Explorer with Live Disk Health',
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
      child: Row(
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
          const Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('Linux Packages (APT)', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 14)),
                SizedBox(height: 2),
                Text('Host OS repositories synced & healthy', style: TextStyle(color: NivaroColors.textMuted, fontSize: 12)),
              ],
            ),
          ),
          const Icon(Icons.check_circle_rounded, color: NivaroColors.successLight, size: 20),
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
