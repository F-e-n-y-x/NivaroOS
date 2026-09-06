import 'dart:math';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/device_sync_service.dart';
import '../services/permission_service.dart';
import '../widgets/common.dart';

class CompanionDevicesScreen extends StatefulWidget {
  const CompanionDevicesScreen({super.key});

  @override
  State<CompanionDevicesScreen> createState() => _CompanionDevicesScreenState();
}

class _CompanionDevicesScreenState extends State<CompanionDevicesScreen> {
  List<CompanionDevice> _devices = [];
  bool _loading = true;
  bool _syncing = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    final list = await DeviceSyncService.instance.listCompanionDevices();
    if (mounted) {
      setState(() {
        _devices = list;
        _loading = false;
      });
    }
  }

  String _formatBytes(int bytes) {
    if (bytes <= 0) return '0 B';
    const suffixes = ['B', 'KB', 'MB', 'GB', 'TB'];
    final i = (log(bytes) / log(1024)).floor().clamp(0, suffixes.length - 1);
    final size = bytes / pow(1024, i);
    return '${size.toStringAsFixed(size >= 10 || i == 0 ? 0 : 1)} ${suffixes[i]}';
  }

  IconData _getBatteryIcon(int level) {
    if (level >= 90) return Icons.battery_full_rounded;
    if (level >= 75) return Icons.battery_6_bar_rounded;
    if (level >= 50) return Icons.battery_5_bar_rounded;
    if (level >= 30) return Icons.battery_3_bar_rounded;
    if (level >= 15) return Icons.battery_2_bar_rounded;
    return Icons.battery_alert_rounded;
  }

  IconData _getDeviceIcon(CompanionDevice dev) {
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

    if (isIos) return Icons.phone_iphone_rounded;
    if (isTablet) return Icons.tablet_android_rounded;
    return Icons.phone_android_rounded;
  }

  Future<void> _syncNow() async {
    HapticFeedback.lightImpact();
    setState(() => _syncing = true);
    await DeviceSyncService.instance.syncWithServer();
    await _load();
    if (mounted) {
      setState(() => _syncing = false);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Companion devices synced with server.'),
          backgroundColor: NivaroColors.success,
        ),
      );
    }
  }

  Future<void> _showRenameDialog(CompanionDevice dev) async {
    final controller = TextEditingController(text: dev.name);
    final formKey = GlobalKey<FormState>();

    final newName = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHigh,
        title: Row(
          children: [
            const Icon(Icons.edit_rounded, color: NivaroColors.primaryLight, size: 22),
            const SizedBox(width: 10),
            Text(dev.isCurrentDevice ? 'Rename This Device' : 'Rename Companion Device',
                style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
          ],
        ),
        content: Form(
          key: formKey,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'Enter a friendly name for ${dev.model}:',
                style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13),
              ),
              const SizedBox(height: 14),
              TextFormField(
                controller: controller,
                autofocus: true,
                style: const TextStyle(color: Colors.white, fontSize: 14),
                decoration: InputDecoration(
                  hintText: 'e.g. Ayush\'s Pixel 8',
                  filled: true,
                  fillColor: NivaroColors.surfaceContainerLowest,
                  border: OutlineInputBorder(borderRadius: BorderRadius.circular(10)),
                ),
                validator: (val) {
                  if (val == null || val.trim().isEmpty) return 'Name cannot be empty';
                  return null;
                },
              ),
            ],
          ),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(
            onPressed: () {
              if (formKey.currentState?.validate() == true) {
                Navigator.pop(ctx, controller.text.trim());
              }
            },
            child: const Text('Save Name'),
          ),
        ],
      ),
    );

    if (newName != null && newName.isNotEmpty && newName != dev.name) {
      HapticFeedback.mediumImpact();
      try {
        await DeviceSyncService.instance.updateRemoteDeviceName(dev.id, newName);
        await _load();
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('Renamed device to "$newName"'),
              backgroundColor: NivaroColors.success,
            ),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Failed to rename device: $e'), backgroundColor: NivaroColors.danger),
          );
        }
      }
    }
  }

  Future<void> _confirmDeleteDevice(CompanionDevice dev) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: NivaroColors.surfaceContainerHigh,
        title: const Text('Disconnect Companion Device', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
        content: Text(
          'Are you sure you want to remove "${dev.name}"? It will need to reconnect to pair again with the server.',
          style: const TextStyle(color: NivaroColors.textMuted, fontSize: 13.5),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: NivaroColors.danger),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Remove Device'),
          ),
        ],
      ),
    );

    if (confirmed == true) {
      try {
        await DeviceSyncService.instance.deleteCompanionDevice(dev.id);
        await _load();
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Removed "${dev.name}"'), backgroundColor: NivaroColors.success),
          );
        }
      } catch (e) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Failed to remove: $e'), backgroundColor: NivaroColors.danger),
          );
        }
      }
    }
  }

  Future<void> _requestStoragePermission() async {
    HapticFeedback.lightImpact();
    final granted = await PermissionService.requestManageStorage();
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(granted ? 'Storage permission granted!' : 'Storage permission denied or required in Settings.'),
          backgroundColor: granted ? NivaroColors.success : NivaroColors.warning,
        ),
      );
      _load();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('Companion Devices', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 18)),
        actions: [
          IconButton(
            icon: _syncing
                ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
                : const Icon(Icons.sync_rounded),
            tooltip: 'Sync Now',
            onPressed: _syncing ? null : _syncNow,
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 1050),
                child: ListView(
                  padding: const EdgeInsets.all(16),
                  children: [
                    const Text('Connected Client Devices', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 16, color: Colors.white)),
                    const SizedBox(height: 6),
                    const Text(
                      'Manage mobile and tablet companion apps connected to your NivaroOS personal cloud server. View storage and rename connected devices.',
                      style: TextStyle(color: NivaroColors.textMuted, fontSize: 13),
                    ),
                    const SizedBox(height: 16),
                    Builder(builder: (context) {
                      final width = MediaQuery.of(context).size.width;
                      final cols = width >= 750 ? 2 : 1;

                      if (cols > 1) {
                        return GridView.builder(
                          shrinkWrap: true,
                          physics: const NeverScrollableScrollPhysics(),
                          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                            crossAxisCount: 2,
                            crossAxisSpacing: 14,
                            mainAxisSpacing: 14,
                            childAspectRatio: 1.35,
                          ),
                          itemCount: _devices.length,
                          itemBuilder: (context, index) => _buildDeviceCard(_devices[index]),
                        );
                      }

                      return Column(
                        children: _devices
                            .map((dev) => Padding(
                                  padding: const EdgeInsets.only(bottom: 14),
                                  child: _buildDeviceCard(dev),
                                ))
                            .toList(),
                      );
                    }),
                    const SizedBox(height: 10),
                    _buildSyncSettingsCard(),
                  ],
                ),
              ),
            ),
    );
  }

  Widget _buildDeviceCard(CompanionDevice dev) {
    final usedStr = _formatBytes(dev.usedStorageBytes);
    final totalStr = _formatBytes(dev.totalStorageBytes);
    final storageRatio = dev.storageUsagePercent;

    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(
          color: dev.isCurrentDevice ? NivaroColors.primary.withOpacity(0.5) : NivaroColors.borderSubtle,
          width: dev.isCurrentDevice ? 1.5 : 1.0,
        ),
        boxShadow: const [
          BoxShadow(color: Color(0x55000000), blurRadius: 10, offset: Offset(0, 3)),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(
                  color: dev.isCurrentDevice ? NivaroColors.primary.withOpacity(0.15) : NivaroColors.surfaceRaised,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Icon(
                  _getDeviceIcon(dev),
                  color: dev.isCurrentDevice ? NivaroColors.primaryLight : Colors.white70,
                  size: 24,
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            dev.name,
                            style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 15.5, color: Colors.white),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                        if (dev.isCurrentDevice) ...[
                          const SizedBox(width: 8),
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                            decoration: BoxDecoration(
                              color: NivaroColors.primary.withOpacity(0.2),
                              borderRadius: BorderRadius.circular(8),
                            ),
                            child: const Text('THIS DEVICE', style: TextStyle(color: NivaroColors.primaryLight, fontSize: 9.5, fontWeight: FontWeight.w800)),
                          ),
                        ],
                      ],
                    ),
                    const SizedBox(height: 3),
                    Text(
                      '${dev.model} · ${dev.platform} · ${dev.appVersion}',
                      style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12),
                    ),
                  ],
                ),
              ),
              IconButton(
                icon: const Icon(Icons.edit_outlined, size: 19, color: NivaroColors.primaryLight),
                tooltip: 'Rename device',
                onPressed: () => _showRenameDialog(dev),
              ),
              if (!dev.isCurrentDevice)
                IconButton(
                  icon: const Icon(Icons.delete_outline_rounded, size: 19, color: NivaroColors.dangerLight),
                  tooltip: 'Disconnect device',
                  onPressed: () => _confirmDeleteDevice(dev),
                ),
            ],
          ),
          const SizedBox(height: 14),
          const Divider(height: 1, color: NivaroColors.borderSubtle),
          const SizedBox(height: 12),

          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Text('Device Storage', style: TextStyle(fontWeight: FontWeight.w700, fontSize: 12.5, color: Colors.white70)),
              Text('$usedStr / $totalStr', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 12.5, color: NivaroColors.primaryLight)),
            ],
          ),
          const SizedBox(height: 8),
          ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: LinearProgressIndicator(
              value: storageRatio,
              minHeight: 6,
              backgroundColor: NivaroColors.surfaceRaised,
              color: NivaroColors.primary,
            ),
          ),
          const SizedBox(height: 8),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              const Row(
                children: [
                  Icon(Icons.cloud_upload_outlined, size: 13, color: NivaroColors.textMuted),
                  SizedBox(width: 4),
                  Text('Backed up to server:', style: TextStyle(color: NivaroColors.textMuted, fontSize: 11.5)),
                ],
              ),
              Text(
                dev.serverStorageUsed > 0 ? _formatBytes(dev.serverStorageUsed) : 'Nothing yet',
                style: TextStyle(
                  color: dev.serverStorageUsed > 0 ? NivaroColors.primaryLight : NivaroColors.textMuted,
                  fontSize: 11.5,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ],
          ),

          const SizedBox(height: 14),

          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Row(
                children: [
                  PulsingStatusDot(
                    color: dev.isOnline ? NivaroColors.success : Colors.white30,
                    size: 7,
                    animate: dev.isOnline,
                  ),
                  const SizedBox(width: 6),
                  Text(
                    dev.isOnline ? 'Online & Syncing' : 'Offline',
                    style: TextStyle(
                      color: dev.isOnline ? NivaroColors.successLight : NivaroColors.textMuted,
                      fontSize: 11.5,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  if (dev.batteryLevel > 0) ...[
                    const SizedBox(width: 12),
                    Icon(_getBatteryIcon(dev.batteryLevel), size: 15, color: dev.batteryLevel <= 20 ? NivaroColors.dangerLight : NivaroColors.textMuted),
                    const SizedBox(width: 3),
                    Text('${dev.batteryLevel}%', style: TextStyle(color: dev.batteryLevel <= 20 ? NivaroColors.dangerLight : NivaroColors.textMuted, fontSize: 11.5, fontWeight: FontWeight.w600)),
                  ],
                ],
              ),
              Text(
                'IP: ${dev.ipAddress}',
                style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _buildSyncSettingsCard() {
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: NivaroColors.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: NivaroColors.borderSubtle),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Row(
            children: [
              Icon(Icons.cloud_sync_rounded, color: NivaroColors.accentLight, size: 22),
              SizedBox(width: 10),
              Text('Shared Cloud Storage Gateway', style: TextStyle(fontWeight: FontWeight.w800, fontSize: 15, color: Colors.white)),
            ],
          ),
          const SizedBox(height: 8),
          const Text(
            'All registered companion devices can access shared device folders in the Files tab. Files pasted to a companion folder are automatically relayed by your NivaroOS server.',
            style: TextStyle(color: NivaroColors.textMuted, fontSize: 12.5, height: 1.4),
          ),
          const SizedBox(height: 14),
          OutlinedButton.icon(
            onPressed: _requestStoragePermission,
            icon: const Icon(Icons.folder_shared_rounded, size: 18),
            label: const Text('Manage Local Storage Permissions'),
            style: OutlinedButton.styleFrom(
              minimumSize: const Size(double.infinity, 42),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
            ),
          ),
        ],
      ),
    );
  }
}
