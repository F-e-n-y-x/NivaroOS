import 'package:flutter/material.dart';

import '../services/api_client.dart';
import '../ui/ui.dart';

/// A starting point for the compose editor.
typedef ComposeTemplate = ({String name, String yaml});

/// Installs an app from a pasted Docker Compose file
/// (`POST /v2/app_management/compose`, YAML body), with a Check that asks
/// the server to validate it first (`dry_run=true`).
class CustomInstallScreen extends StatefulWidget {
  const CustomInstallScreen({super.key, this.initialYaml});

  /// Text to start the editor with.
  final String? initialYaml;

  static const List<ComposeTemplate> templates = [
    (
      name: 'Nginx',
      yaml: 'name: nginx\n'
          'services:\n'
          '  web:\n'
          '    image: nginx:alpine\n'
          '    restart: unless-stopped\n'
          '    ports:\n'
          '      - "8080:80"\n'
          '    volumes:\n'
          '      - /DATA/AppData/nginx/html:/usr/share/nginx/html\n',
    ),
    (
      name: 'Redis',
      yaml: 'name: redis\n'
          'services:\n'
          '  redis:\n'
          '    image: redis:alpine\n'
          '    restart: unless-stopped\n'
          '    ports:\n'
          '      - "6379:6379"\n'
          '    volumes:\n'
          '      - /DATA/AppData/redis:/data\n',
    ),
    (
      name: 'PostgreSQL',
      yaml: 'name: postgres\n'
          'services:\n'
          '  db:\n'
          '    image: postgres:16-alpine\n'
          '    restart: unless-stopped\n'
          '    environment:\n'
          '      POSTGRES_USER: postgres\n'
          '      POSTGRES_PASSWORD: change-me\n'
          '      POSTGRES_DB: app\n'
          '    ports:\n'
          '      - "5432:5432"\n'
          '    volumes:\n'
          '      - /DATA/AppData/postgres:/var/lib/postgresql/data\n',
    ),
  ];

  @override
  State<CustomInstallScreen> createState() => _CustomInstallScreenState();
}

enum _Busy { none, checking, installing }

class _CustomInstallScreenState extends State<CustomInstallScreen> {
  late final _yaml = TextEditingController(text: widget.initialYaml ?? '');
  _Busy _busy = _Busy.none;

  /// The last Check or Install answer: true = valid, a message = what's wrong.
  Object? _result;

  @override
  void initState() {
    super.initState();
    _yaml.addListener(() {
      if (_result != null) setState(() => _result = null);
      setState(() {});
    });
  }

  @override
  void dispose() {
    _yaml.dispose();
    super.dispose();
  }

  bool get _isTemplate => CustomInstallScreen.templates.any((t) => t.yaml == _yaml.text);

  Future<void> _useTemplate(ComposeTemplate t) async {
    if (_yaml.text.trim().isNotEmpty && !_isTemplate) {
      final ok = await ConfirmDialog.destructive(
        context,
        title: 'Replace your text?',
        message: 'The ${t.name} example replaces what is in the editor.',
        confirmLabel: 'Replace',
      );
      if (!ok) return;
    }
    _yaml.text = t.yaml;
  }

  Future<void> _submit({required bool dryRun}) async {
    final yaml = _yaml.text.trim();
    if (yaml.isEmpty) return;
    FocusScope.of(context).unfocus();
    setState(() => _busy = dryRun ? _Busy.checking : _Busy.installing);
    final nav = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ApiClient.instance.postBody('/v2/app_management/compose', '$yaml\n', 'application/yaml', query: {
        'check_port_conflict': 'true',
        if (dryRun) 'dry_run': 'true',
      });
      if (!mounted) return;
      if (dryRun) {
        setState(() => _result = true);
      } else {
        final name = RegExp(r'^name:\s*(\S+)', multiLine: true).firstMatch(yaml)?.group(1);
        messenger.showSnackBar(SnackBar(content: Text('Installing ${name ?? 'the app'}. It appears in Apps when it is ready.')));
        nav.pop(true);
      }
    } catch (e) {
      if (mounted) setState(() => _result = e is ApiException ? e.message : e.toString());
    } finally {
      if (mounted) setState(() => _busy = _Busy.none);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final gutter = Space.gutter(context);
    final empty = _yaml.text.trim().isEmpty;
    final result = _result;
    final busy = _busy != _Busy.none;

    return AppScaffold(
      title: 'Install from compose file',
      body: Column(children: [
        Expanded(
          child: ListView(
        padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.xl),
        children: [
          Text(
            'Paste a Docker Compose file. NivaroOS installs it as an app, with its own folder under /DATA/AppData.',
            style: theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant),
          ),
          const SizedBox(height: Space.lg),
          Text('Start from an example', style: theme.textTheme.titleSmall?.copyWith(color: scheme.primary)),
          const SizedBox(height: Space.sm),
          Wrap(
            spacing: Space.sm,
            runSpacing: Space.sm,
            children: [
              for (final t in CustomInstallScreen.templates) ActionChip(label: Text(t.name), onPressed: busy ? null : () => _useTemplate(t)),
            ],
          ),
          const SizedBox(height: Space.lg),
          TextField(
            controller: _yaml,
            minLines: 12,
            maxLines: null,
            enabled: !busy,
            keyboardType: TextInputType.multiline,
            autocorrect: false,
            enableSuggestions: false,
            style: theme.textTheme.bodyMedium?.copyWith(fontFamily: 'monospace'),
            decoration: InputDecoration(
              labelText: 'Compose file',
              // The label stays above the box, so the example below it
              // reads as an example, not as a label.
              floatingLabelBehavior: FloatingLabelBehavior.always,
              alignLabelWithHint: true,
              hintText: 'services:\n  app:\n    image: …',
              hintStyle: theme.textTheme.bodyMedium?.copyWith(fontFamily: 'monospace', color: scheme.onSurfaceVariant),
              errorText: result is String ? result : null,
              errorMaxLines: 6,
              helperText: result == true ? 'The server accepted this file.' : null,
            ),
          ),
        ],
          ),
        ),
        // The actions stay put at the bottom rather than floating under a
        // box of any height.
        Material(
          color: scheme.surfaceContainer,
          child: SafeArea(
            top: false,
            child: Padding(
              padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, Space.md),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  OutlinedButton(
                    onPressed: empty || busy ? null : () => _submit(dryRun: true),
                    child: Text(_busy == _Busy.checking ? 'Checking…' : 'Check'),
                  ),
                  const SizedBox(width: Space.sm),
                  FilledButton(
                    onPressed: empty || busy ? null : () => _submit(dryRun: false),
                    child: Text(_busy == _Busy.installing ? 'Installing…' : 'Install'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ]),
    );
  }
}
