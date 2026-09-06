import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme.dart';
import '../services/api_client.dart';

class ContainerLogsScreen extends StatefulWidget {
  final String appId;
  final String appTitle;

  const ContainerLogsScreen({super.key, required this.appId, required this.appTitle});

  @override
  State<ContainerLogsScreen> createState() => _ContainerLogsScreenState();
}

class _ContainerLogsScreenState extends State<ContainerLogsScreen> {
  final _scrollController = ScrollController();
  final _searchController = TextEditingController();
  String _logs = '';
  bool _loading = true;
  bool _autoScroll = true;
  Timer? _pollTimer;

  @override
  void initState() {
    super.initState();
    _loadLogs();
    _pollTimer = Timer.periodic(const Duration(seconds: 3), (_) => _loadLogs(silent: true));
    _searchController.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    _scrollController.dispose();
    _searchController.dispose();
    super.dispose();
  }

  Future<void> _loadLogs({bool silent = false}) async {
    if (!silent) setState(() => _loading = true);
    try {
      final res = await ApiClient.instance.get('/v2/app_management/compose/${widget.appId}/logs');
      final data = res['data']?.toString() ?? '';
      if (mounted) {
        setState(() {
          _logs = data;
          _loading = false;
        });
        if (_autoScroll && _scrollController.hasClients) {
          _scrollController.jumpTo(_scrollController.position.maxScrollExtent);
        }
      }
    } catch (_) {
      if (mounted && !silent) setState(() => _loading = false);
    }
  }

  List<String> get _filteredLines {
    final query = _searchController.text.trim().toLowerCase();
    final lines = _logs.split('\n');
    if (query.isEmpty) return lines;
    return lines.where((l) => l.toLowerCase().contains(query)).toList();
  }

  void _copyAll() {
    Clipboard.setData(ClipboardData(text: _logs));
    HapticFeedback.lightImpact();
    ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Logs copied to clipboard.')));
  }

  @override
  Widget build(BuildContext context) {
    final lines = _filteredLines;

    return Scaffold(
      backgroundColor: NivaroColors.background,
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('${widget.appTitle} Logs', style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 16)),
            Text(widget.appId, style: const TextStyle(color: NivaroColors.textMuted, fontSize: 11)),
          ],
        ),
        actions: [
          IconButton(
            icon: Icon(_autoScroll ? Icons.arrow_downward_rounded : Icons.pause_circle_outline_rounded,
                color: _autoScroll ? NivaroColors.primaryLight : NivaroColors.textMuted),
            tooltip: _autoScroll ? 'Auto-scroll ON' : 'Auto-scroll OFF',
            onPressed: () => setState(() => _autoScroll = !_autoScroll),
          ),
          IconButton(
            icon: const Icon(Icons.copy_rounded, size: 20),
            tooltip: 'Copy all',
            onPressed: _copyAll,
          ),
          IconButton(
            icon: const Icon(Icons.refresh_rounded, size: 20),
            tooltip: 'Refresh',
            onPressed: () => _loadLogs(),
          ),
        ],
      ),
      body: Column(
        children: [
          // Search filter bar
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            color: NivaroColors.surfaceContainerLowest,
            child: TextField(
              controller: _searchController,
              decoration: InputDecoration(
                hintText: 'Filter log lines...',
                prefixIcon: const Icon(Icons.filter_list_rounded, size: 18),
                suffixIcon: _searchController.text.isNotEmpty
                    ? IconButton(
                        icon: const Icon(Icons.clear_rounded, size: 16),
                        onPressed: () => _searchController.clear(),
                      )
                    : null,
                contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
              ),
            ),
          ),
          // Log output view
          Expanded(
            child: _loading && _logs.isEmpty
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
                          } else if (line.toLowerCase().contains('info')) {
                            textColor = NivaroColors.infoLight;
                          }

                          return Text(
                            line,
                            style: TextStyle(
                              fontFamily: 'monospace',
                              fontSize: 11.5,
                              color: textColor,
                              height: 1.4,
                            ),
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
