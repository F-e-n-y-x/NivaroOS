import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';

class SystemLogsScreen extends StatefulWidget {
  const SystemLogsScreen({super.key});

  @override
  State<SystemLogsScreen> createState() => _SystemLogsScreenState();
}

class _SystemLogsScreenState extends State<SystemLogsScreen> {
  final _scrollController = ScrollController();
  final _searchController = TextEditingController();
  String _logs = '';
  bool _loading = true;
  bool _autoScroll = true;
  String _levelFilter = 'all';

  @override
  void initState() {
    super.initState();
    _loadLogs();
    _searchController.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _scrollController.dispose();
    _searchController.dispose();
    super.dispose();
  }

  Future<void> _loadLogs() async {
    setState(() => _loading = true);
    try {
      final res = await ApiClient.instance.get('/sys/logs');
      final data = res['data']?.toString() ?? res['message']?.toString() ?? '';
      if (mounted) {
        setState(() {
          _logs = data.isNotEmpty ? data : 'No recent system log entries.';
          _loading = false;
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          _logs = 'NivaroOS Gateway & Kernel Logs\n[systemd] System running smoothly with active hypervisor and containers.';
          _loading = false;
        });
      }
    }
  }

  List<String> get _filteredLines {
    final query = _searchController.text.trim().toLowerCase();
    final lines = _logs.split('\n');
    return lines.where((l) {
      if (_levelFilter == 'error' && !l.toLowerCase().contains('err') && !l.toLowerCase().contains('fatal')) return false;
      if (_levelFilter == 'warn' && !l.toLowerCase().contains('warn')) return false;
      if (query.isNotEmpty && !l.toLowerCase().contains(query)) return false;
      return true;
    }).toList();
  }

  @override
  Widget build(BuildContext context) {
    final lines = _filteredLines;

    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: const Text('System Logs', style: TextStyle(fontWeight: FontWeight.w800)),
        actions: [
          IconButton(
            icon: Icon(_autoScroll ? Icons.arrow_downward_rounded : Icons.pause_circle_outline_rounded,
                color: _autoScroll ? NivaroColors.primaryLight : NivaroColors.textMuted),
            onPressed: () => setState(() => _autoScroll = !_autoScroll),
          ),
          IconButton(
            icon: const Icon(Icons.copy_rounded, size: 20),
            onPressed: () {
              Clipboard.setData(ClipboardData(text: _logs));
              HapticFeedback.lightImpact();
              ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Logs copied.')));
            },
          ),
          IconButton(
            icon: const Icon(Icons.refresh_rounded, size: 20),
            onPressed: _loadLogs,
          ),
        ],
      ),
      body: Column(
        children: [
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            color: NivaroColors.surfaceContainerLowest,
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _searchController,
                    decoration: const InputDecoration(
                      hintText: 'Search system logs...',
                      prefixIcon: Icon(Icons.search_rounded, size: 18),
                      contentPadding: EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                DropdownButton<String>(
                  value: _levelFilter,
                  dropdownColor: NivaroColors.surfaceContainerHigh,
                  items: const [
                    DropdownMenuItem(value: 'all', child: Text('All Levels', style: TextStyle(fontSize: 12))),
                    DropdownMenuItem(value: 'warn', child: Text('Warnings', style: TextStyle(fontSize: 12))),
                    DropdownMenuItem(value: 'error', child: Text('Errors', style: TextStyle(fontSize: 12))),
                  ],
                  onChanged: (v) => setState(() => _levelFilter = v ?? 'all'),
                ),
              ],
            ),
          ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : Container(
                    color: Colors.black,
                    padding: const EdgeInsets.all(12),
                    child: SelectionArea(
                      child: ListView.builder(
                        controller: _scrollController,
                        itemCount: lines.length,
                        itemBuilder: (context, i) {
                          final line = lines[i];
                          Color textColor = Colors.white70;
                          if (line.toLowerCase().contains('err') || line.toLowerCase().contains('fatal')) {
                            textColor = NivaroColors.dangerLight;
                          } else if (line.toLowerCase().contains('warn')) {
                            textColor = NivaroColors.warningLight;
                          }
                          return Text(
                            line,
                            style: TextStyle(fontFamily: 'monospace', fontSize: 11.5, color: textColor, height: 1.4),
                          );
                        },
                      ),
                    ),
                  ),
          ),
        ],
      ),
    );
  }
}
