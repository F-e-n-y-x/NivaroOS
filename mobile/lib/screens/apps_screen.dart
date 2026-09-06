import 'package:flutter/material.dart';
import '../theme.dart';
import '../services/api_client.dart';
import '../models/container_entry.dart';
import '../widgets/common.dart';

/// The "main features visible in the app" tab: installed containers/apps.
/// Read-only for v1 - `GET /v1/container/all` gives name/image/state/status
/// but no icon or web-UI URL (verified against the actual Go route; NivaroOS
/// has no CasaOS-style app catalog endpoint with those fields yet), so this
/// intentionally doesn't promise a one-tap "open this app's web UI" launcher
/// it can't back with real data - it shows what's actually running instead
/// of guessing at a URL scheme.
class AppsScreen extends StatefulWidget {
  const AppsScreen({super.key});

  @override
  State<AppsScreen> createState() => _AppsScreenState();
}

class _AppsScreenState extends State<AppsScreen> {
  List<ContainerEntry> _containers = [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final res = await ApiClient.instance.get('/container/all');
      final data = res['data'] as List<dynamic>? ?? [];
      final containers = data.map((e) => ContainerEntry.fromJson(e as Map<String, dynamic>)).toList();
      containers.sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
      if (!mounted) return;
      setState(() {
        _containers = containers;
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

  Color _stateColor(String state) {
    switch (state) {
      case 'running':
        return NivaroColors.success;
      case 'paused':
        return NivaroColors.warning;
      case 'exited':
      case 'dead':
        return NivaroColors.textFaint;
      default:
        return NivaroColors.textMuted;
    }
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      bottom: false,
      child: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 12, 20, 140),
          children: [
            Row(
              children: [
                const Expanded(child: Text('Apps', style: nivaroTitleStyle)),
                RoundIconButton(icon: Icons.refresh_rounded, onPressed: _load),
              ],
            ),
            const SizedBox(height: 24),
            if (_loading)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 60),
                child: Center(child: CircularProgressIndicator()),
              )
            else if (_error != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 60),
                child: Center(child: Text(_error!, style: const TextStyle(color: NivaroColors.textMuted), textAlign: TextAlign.center)),
              )
            else if (_containers.isEmpty)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 60),
                child: Center(child: Text('No installed apps found.', style: TextStyle(color: NivaroColors.textMuted))),
              )
            else
              ..._containers.map((c) => Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: DarkCard(
                      child: Row(
                        children: [
                          Container(
                            width: 44,
                            height: 44,
                            decoration: BoxDecoration(color: _stateColor(c.state).withOpacity(0.16), borderRadius: BorderRadius.circular(14)),
                            alignment: Alignment.center,
                            child: Icon(Icons.widgets_rounded, color: _stateColor(c.state), size: 22),
                          ),
                          const SizedBox(width: 14),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(c.name, style: const TextStyle(fontWeight: FontWeight.w700, fontSize: 15), maxLines: 1, overflow: TextOverflow.ellipsis),
                                const SizedBox(height: 2),
                                Text(c.image, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 12), maxLines: 1, overflow: TextOverflow.ellipsis),
                              ],
                            ),
                          ),
                          Column(
                            crossAxisAlignment: CrossAxisAlignment.end,
                            children: [
                              Text(c.state, style: TextStyle(color: _stateColor(c.state), fontWeight: FontWeight.w600, fontSize: 12.5)),
                              if (c.hasUpdate) ...[
                                const SizedBox(height: 4),
                                const Text('Update available', style: TextStyle(color: NivaroColors.warning, fontSize: 11)),
                              ],
                            ],
                          ),
                        ],
                      ),
                    ),
                  )),
          ],
        ),
      ),
    );
  }
}
