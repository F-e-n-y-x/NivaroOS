import 'package:flutter/material.dart';

import '../../services/api_client.dart';
import '../../services/download_station_api.dart';
import '../../ui/ui.dart';
import 'ds_folder_picker.dart';

/// Download Station's download settings, as on the web's Settings panel:
/// default folder, connections per download, simultaneous downloads and
/// the shared speed limit. Each change is saved at once.
class DsSettingsScreen extends StatefulWidget {
  const DsSettingsScreen({super.key, this.api});

  final DownloadStationApi? api;

  @override
  State<DsSettingsScreen> createState() => _DsSettingsScreenState();
}

class _DsSettingsScreenState extends State<DsSettingsScreen> {
  late final DownloadStationApi _api = widget.api ?? DownloadStationApi();
  DsSettings? _s;
  Object? _error;

  static const _mib = 1024 * 1024;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final s = await _api.settings();
      if (mounted) setState(() => _s = s);
    } catch (e) {
      if (mounted) setState(() => _error = e);
    }
  }

  Future<void> _save(Map<String, Object> changes) async {
    try {
      final s = await _api.saveSettings(changes);
      if (mounted) setState(() => _s = s);
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text("Couldn't save: $e")));
      await _load();
    }
  }

  Future<void> _editLimit(DsSettings s) async {
    final ctrl = TextEditingController(text: s.speedLimit == 0 ? '0' : (s.speedLimit / _mib).toStringAsFixed(1).replaceFirst(RegExp(r'\.0$'), ''));
    final v = await showDialog<double>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Speed limit'),
        content: TextField(
          controller: ctrl,
          autofocus: true,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          decoration: const InputDecoration(suffixText: 'MiB/s', helperText: 'Shared by all downloads. 0 is unlimited.'),
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(context).pop(), child: const Text('Cancel')),
          TextButton(onPressed: () => Navigator.of(context).pop(double.tryParse(ctrl.text.replaceAll(',', '.'))), child: const Text('Save')),
        ],
      ),
    );
    ctrl.dispose();
    if (v == null || v < 0) return;
    await _save({'speed_limit': (v * _mib).round()});
  }

  @override
  Widget build(BuildContext context) {
    final s = _s;
    final Widget body;
    if (s == null && _error != null) {
      final e = _error!;
      body = e is ApiException && e.isUnreachable
          ? ErrorState.offline(onRetry: _load)
          : ErrorState(title: "Couldn't load the settings", message: e.toString(), onRetry: _load);
    } else if (s == null) {
      body = const LoadingList(rows: 4);
    } else {
      body = ListView(children: [
        TileGroup(title: 'Downloads', children: [
          ListTile(
            leading: const Icon(Icons.folder_outlined),
            title: const Text('Default save folder'),
            subtitle: Text(s.defaultDir),
            trailing: const Icon(Icons.chevron_right),
            onTap: () async {
              final p = await pickDownloadFolder(context, start: s.defaultDir, api: _api);
              if (p != null) await _save({'default_dir': p});
            },
          ),
          _SliderTile(
            icon: Icons.lan_outlined,
            title: 'Connections per download',
            subtitle: 'Default for new downloads. Each connection fetches its own part of the file.',
            value: s.defaultConnections,
            min: 1,
            max: 32,
            onChanged: (v) => _save({'default_connections': v}),
          ),
          _SliderTile(
            icon: Icons.download_outlined,
            title: 'Simultaneous downloads',
            subtitle: 'More than this wait in the queue.',
            value: s.maxConcurrent,
            min: 1,
            max: 20,
            onChanged: (v) => _save({'max_concurrent': v}),
          ),
          ListTile(
            leading: const Icon(Icons.speed_outlined),
            title: const Text('Speed limit'),
            subtitle: Text(s.speedLimit == 0 ? 'Unlimited' : '${(s.speedLimit / _mib).toStringAsFixed(1)} MiB/s, shared by all downloads'),
            trailing: const Icon(Icons.chevron_right),
            onTap: () => _editLimit(s),
          ),
        ]),
      ]);
    }
    return AppScaffold(title: 'Download Station settings', onRefresh: _load, body: body);
  }
}

/// A setting picked on a slider, saved when the thumb is let go.
class _SliderTile extends StatefulWidget {
  const _SliderTile({required this.icon, required this.title, required this.subtitle, required this.value, required this.min, required this.max, required this.onChanged});

  final IconData icon;
  final String title;
  final String subtitle;
  final int value;
  final int min;
  final int max;
  final ValueChanged<int> onChanged;

  @override
  State<_SliderTile> createState() => _SliderTileState();
}

class _SliderTileState extends State<_SliderTile> {
  double? _dragging;

  @override
  Widget build(BuildContext context) {
    final v = _dragging ?? widget.value.toDouble();
    return ListTile(
      leading: Icon(widget.icon),
      title: Text(widget.title),
      trailing: Text('${v.round()}', style: Theme.of(context).textTheme.titleMedium?.tabular),
      subtitle: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
        Text(widget.subtitle),
        Slider(
          value: v.clamp(widget.min.toDouble(), widget.max.toDouble()),
          min: widget.min.toDouble(),
          max: widget.max.toDouble(),
          divisions: widget.max - widget.min,
          label: '${v.round()}',
          onChanged: (x) => setState(() => _dragging = x),
          onChangeEnd: (x) {
            setState(() => _dragging = null);
            if (x.round() != widget.value) widget.onChanged(x.round());
          },
        ),
      ]),
    );
  }
}
