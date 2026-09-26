import 'package:flutter/material.dart';

import '../models/compose_edit.dart';
import '../models/container_entry.dart';
import '../services/api_client.dart';
import '../ui/ui.dart';
import '../utils/app_icons.dart';
import 'apps_screen.dart';

enum _Mode { form, yaml }

/// What Edit app pops once the server has taken the new settings.
class AppEditSaved {
  const AppEditSaved({required this.recreates});

  /// False when only the app's name, icon or Web UI link changed (the
  /// project's x-casaos): the server saves them without recreating the
  /// containers, so the app never goes down.
  final bool recreates;
}

/// Why Save didn't go through.
enum _SaveErrorKind { form, server }

/// Edit app (owner request, 2026-09-26): an installed compose app's
/// settings - name, icon, the Web UI link, and each service's image, tag,
/// restart policy, ports, folders, variables, devices, network and limits -
/// plus the whole compose file for anything else. The same API as the web
/// dashboard's app settings form: the compose file from
/// `GET …/compose/{id}` as YAML, saved with `PUT …/compose/{id}`, after
/// which the server pulls the images and recreates the app.
///
/// Pops an [AppEditSaved] once the server has taken the new settings.
class AppEditScreen extends StatefulWidget {
  const AppEditScreen({super.key, required this.app});

  final InstalledApp app;

  @override
  State<AppEditScreen> createState() => _AppEditScreenState();
}

class _AppEditScreenState extends State<AppEditScreen> {
  final _formKey = GlobalKey<FormState>();
  final _scroll = ScrollController();
  final _yaml = TextEditingController();
  final _yamlFocus = FocusNode();

  /// The Web UI port, which the published-ports menu also sets.
  final _port = TextEditingController();

  bool _loading = true;
  Object? _loadError;

  /// The file as the server sent it, parsed, and as text.
  Map<String, Object?>? _base;
  String _baseText = '';

  /// The document the form is a view of: [_base], or what was typed in the
  /// compose file editor.
  Map<String, Object?>? _doc;
  ComposeForm? _form;

  /// Bumped when the form is rebuilt from another document, so the fields
  /// start again from its values.
  int _generation = 0;

  _Mode _mode = _Mode.form;
  String? _yamlError;
  bool _saving = false;
  String? _saveError;
  _SaveErrorKind _saveErrorKind = _SaveErrorKind.server;

  /// The form's problems when Save was refused, each a tap target.
  List<FormIssue> _saveIssues = const [];
  final Set<EnvRow> _revealed = {};

  /// Each text field's state and focus, by `$_generation/fieldId`, so a
  /// problem can scroll to its field.
  final _fields = <String, GlobalKey<FormFieldState<String>>>{};
  final _focus = <String, FocusNode>{};

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _scroll.dispose();
    _yaml.dispose();
    _yamlFocus.dispose();
    _port.dispose();
    for (final f in _focus.values) {
      f.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    try {
      final text = await AppsApi.composeFile(widget.app);
      final doc = parseComposeYaml(text);
      if (!mounted) return;
      setState(() {
        _baseText = text;
        _base = doc;
        _doc = doc;
        _form = ComposeForm.fromDoc(doc);
        _port.text = _form!.port;
        _generation++;
      });
    } catch (e) {
      if (mounted) setState(() => _loadError = e);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  String get _title {
    final t = _form?.title.trim() ?? '';
    return t.isEmpty ? widget.app.title : t;
  }

  /// The document Save would send, or null when the compose file editor
  /// holds something that doesn't parse.
  Map<String, Object?>? _current() {
    if (_mode == _Mode.yaml) {
      try {
        return parseComposeYaml(_yaml.text);
      } on FormatException {
        return null;
      }
    }
    final form = _form;
    final doc = _doc;
    return form == null || doc == null ? null : form.applyTo(doc);
  }

  bool get _dirty {
    final base = _base;
    if (base == null) return false;
    if (_mode == _Mode.yaml && _yaml.text != _baseText) {
      final cur = _current();
      return cur == null || !deepEquals(cur, base);
    }
    final cur = _current();
    return cur != null && !deepEquals(cur, base);
  }

  void _edit(VoidCallback change) => setState(() {
    change();
    _refreshSaveError();
  });

  /// After Save was refused for the form's problems, the banner follows the
  /// edits: it lists what is still wrong and goes once nothing is.
  void _refreshSaveError() {
    if (_saveError == null || _saveErrorKind != _SaveErrorKind.form || _form == null) return;
    // The fields' validators are the same checks as issues(), so this is
    // what they show once they have rebuilt.
    final issues = _form!.issues();
    if (issues.isEmpty) {
      _saveError = null;
      _saveIssues = const [];
    } else {
      _saveError = _formErrorText(issues);
      _saveIssues = issues;
    }
  }

  /// A change to a published port may settle "ports in use" from the
  /// server, so that message goes with it.
  void _portEdited() {
    if (_saveErrorKind == _SaveErrorKind.server) _saveError = null;
  }

  static String _formErrorText(List<FormIssue> issues) => issues.isEmpty
      ? 'Some settings need fixing. They are marked below.'
      : issues.length == 1
      ? issues.first.message
      : '${issues.length} settings need fixing. Tap one to go to it.';

  /// Scrolls to [field] (a [fieldId]) and puts the cursor in it.
  Future<void> _goTo(String field) async {
    final key = _fields['$_generation/$field'];
    final ctx = key?.currentContext;
    if (ctx == null) return;
    await Scrollable.ensureVisible(ctx, alignment: .3, duration: Motion.of(context).medium, curve: Curves.easeOutCubic);
    _focus['$_generation/$field']?.requestFocus();
  }

  /// The first field (from the top) that shows an error or has a problem.
  String? _firstBadField(List<FormIssue> issues) {
    final prefix = '$_generation/';
    final bad = <String>{
      for (final i in issues) i.field,
      for (final e in _fields.entries)
        if (e.key.startsWith(prefix) && (e.value.currentState?.hasError ?? false)) e.key.substring(prefix.length),
    };
    String? best;
    var bestY = double.infinity;
    for (final id in bad) {
      final box = _fields['$prefix$id']?.currentContext?.findRenderObject();
      if (box is! RenderBox || !box.attached) continue;
      final y = box.localToGlobal(Offset.zero).dy;
      if (y < bestY) {
        bestY = y;
        best = id;
      }
    }
    return best;
  }

  /// The document as saved: does it change more than the project's
  /// x-casaos (name, icon, Web UI link)?
  bool _recreates(Map<String, Object?>? saved) {
    final base = _base;
    if (saved == null || base == null) return true;
    Map<String, Object?> rest(Map<String, Object?> d) => Map.of(d)..remove('x-casaos');
    return !deepEquals(rest(saved), rest(base));
  }

  void _switchMode(_Mode to) {
    if (to == _mode) return;
    if (to == _Mode.yaml) {
      final cur = _current();
      final base = _base;
      setState(() {
        // Unchanged: the server's own text, not a re-written copy.
        _yaml.text = cur != null && base != null && deepEquals(cur, base) ? _baseText : emitYaml(cur);
        _yamlError = null;
        _mode = _Mode.yaml;
      });
      return;
    }
    final error = validateComposeText(_yaml.text, _form?.appName ?? widget.app.id);
    if (error != null) {
      setState(() => _yamlError = error);
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Fix the compose file first, then switch back to the form')));
      return;
    }
    final doc = parseComposeYaml(_yaml.text);
    setState(() {
      _doc = doc;
      _form = ComposeForm.fromDoc(doc);
      _port.text = _form!.port;
      _generation++;
      _mode = _Mode.form;
    });
  }

  Future<void> _save() async {
    FocusScope.of(context).unfocus();
    final String text;
    if (_mode == _Mode.yaml) {
      final error = validateComposeText(_yaml.text, _form?.appName ?? widget.app.id);
      if (error != null) {
        setState(() => _yamlError = error);
        return;
      }
      text = _yaml.text;
    } else {
      final valid = _formKey.currentState?.validate() ?? false;
      final issues = _form!.issues();
      if (!valid || issues.isNotEmpty) {
        setState(() {
          _saveError = _formErrorText(issues);
          _saveErrorKind = _SaveErrorKind.form;
          _saveIssues = issues;
        });
        // Save is in the top bar and the form is long: go to the first
        // field to fix, wherever it is.
        final first = _firstBadField(issues);
        if (first != null) WidgetsBinding.instance.addPostFrameCallback((_) => mounted ? _goTo(first) : null);
        return;
      }
      text = emitYaml(_current());
    }
    if (!_dirty) return;
    Map<String, Object?>? saved;
    try {
      saved = parseComposeYaml(text);
    } on FormatException {
      saved = null;
    }
    final recreates = _recreates(saved);
    final go = recreates
        ? await ConfirmDialog.confirm(
            context,
            title: 'Save and restart $_title?',
            message:
                '$_title is recreated with the new settings, so it is offline for a moment - longer if the server has to '
                'download a new image first. Its data folders are kept.',
            confirmLabel: 'Save and restart',
          )
        : await ConfirmDialog.confirm(
            context,
            title: 'Save the changes to $_title?',
            message: 'Only its name, icon or Web UI link changed, so $_title keeps running.',
            confirmLabel: 'Save',
          );
    if (!go || !mounted) return;
    // Closing the dialog gives the focus back to the last field, which
    // would scroll the page to it rather than to an error at the top.
    FocusManager.instance.primaryFocus?.unfocus();
    setState(() {
      _saving = true;
      _saveError = null;
      _saveIssues = const [];
    });
    final nav = Navigator.of(context);
    try {
      await AppsApi.saveComposeFile(widget.app, text);
      if (!mounted) return;
      setState(() => _saving = false);
      nav.pop(AppEditSaved(recreates: recreates));
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _saveError = saveErrorText(e);
        _saveErrorKind = _SaveErrorKind.server;
      });
    }
  }

  Future<void> _onPop(bool didPop, Object? result) async {
    if (didPop || _saving) return;
    final discard = await ConfirmDialog.destructive(
      context,
      title: 'Discard changes?',
      message: 'Your changes to $_title are not saved.',
      confirmLabel: 'Discard',
      permanent: false,
    );
    if (discard && mounted) Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final ready = !_loading && _loadError == null && _form != null;
    return PopScope(
      canPop: !_saving && !_dirty,
      onPopInvokedWithResult: _onPop,
      child: AppScaffold(
        title: 'Edit ${widget.app.title}',
        leading: IconButton(tooltip: 'Close', icon: const Icon(Icons.close), onPressed: () => Navigator.of(context).maybePop()),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: Space.sm),
            child: _saving
                ? Semantics(
                    label: 'Saving',
                    child: const Padding(
                      padding: EdgeInsets.all(Space.md),
                      child: SizedBox.square(dimension: 24, child: CircularProgressIndicator(strokeWidth: 2.5)),
                    ),
                  )
                // Only once something changed: an untouched form has
                // nothing to save.
                : TextButton(onPressed: ready && _dirty ? _save : null, child: const Text('Save')),
          ),
        ],
        body: _body(context),
      ),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) return const LoadingList(rows: 8, leading: SkeletonLeading.none);
    final error = _loadError;
    if (error != null) {
      if (error is ApiException && error.isUnreachable) return ErrorState.offline(onRetry: _load, details: error.details);
      return ErrorState(
        title: "Couldn't load the settings of ${widget.app.title}",
        message: error is FormatException ? "The server's compose file can't be read: ${error.message}" : _reason(error),
        details: error is ApiException ? error.details : error.toString(),
        onRetry: _load,
      );
    }
    final gutter = Space.gutter(context);
    // The progress and a save error stay above the form, in view
    // wherever it is scrolled to.
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (_saving) const LinearProgressIndicator(semanticsLabel: 'Saving'),
        AnimatedSize(
          duration: Motion.of(context).short,
          alignment: Alignment.topCenter,
          child: _saveError == null ? const SizedBox(width: double.infinity) : _saveBanner(context),
        ),
        Expanded(
          child: AbsorbPointer(
            absorbing: _saving,
            child: Form(
              key: _formKey,
              // Every field is built, not only those in view: Save
              // validates them all and can scroll to the first bad one.
              child: SingleChildScrollView(
                controller: _scroll,
                padding: const EdgeInsets.only(bottom: Space.xxl),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                  Padding(
                    padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
                    child: SegmentedButton<_Mode>(
                      segments: const [
                        ButtonSegment(value: _Mode.form, icon: Icon(Icons.tune_outlined), label: Text('Settings')),
                        ButtonSegment(value: _Mode.yaml, icon: Icon(Icons.code), label: Text('Compose file')),
                      ],
                      selected: {_mode},
                      showSelectedIcon: false,
                      onSelectionChanged: (s) => _switchMode(s.first),
                    ),
                  ),
                  if (_mode == _Mode.form) ..._formChildren(context) else ..._yamlChildren(context),
                  ],
                ),
              ),
            ),
          ),
        ),
      ],
    );
  }

  Widget _saveBanner(BuildContext context) {
    final issues = _saveErrorKind == _SaveErrorKind.form ? _saveIssues : const <FormIssue>[];
    if (issues.length == 1) {
      return Notice(
        title: "Couldn't save",
        message: _saveError!,
        status: Status.error,
        actionLabel: 'Show',
        onAction: () => _goTo(issues.first.field),
      );
    }
    final style = Notice.detailStyle(context, Status.error);
    const shown = 3;
    return Notice(
      title: "Couldn't save",
      message: _saveError!,
      status: Status.error,
      details: issues.isEmpty
          ? null
          : [
              const SizedBox(height: Space.xs),
              for (final i in issues.take(shown))
                InkWell(
                  onTap: () => _goTo(i.field),
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(minHeight: 44),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(i.message, style: style, maxLines: 2, overflow: TextOverflow.ellipsis),
                        ),
                        Icon(Icons.chevron_right, color: style?.color),
                      ],
                    ),
                  ),
                ),
              if (issues.length > shown) Text('and ${issues.length - shown} more, marked below', style: style),
            ],
    );
  }

  // --- The compose file editor ---------------------------------------------

  List<Widget> _yamlChildren(BuildContext context) {
    final theme = Theme.of(context);
    final tokens = DesignTokens.of(context);
    final gutter = Space.gutter(context);
    final mono = tokens.mono(theme.textTheme.bodyMedium);
    // Lines don't wrap: indentation is the structure of a compose file, and
    // a wrapped line would read as a new key. Wide files scroll sideways.
    final painter = TextPainter(
      text: TextSpan(text: 'M', style: mono),
      textDirection: TextDirection.ltr,
      textScaler: MediaQuery.textScalerOf(context),
    )..layout();
    final longest = _yaml.text.split('\n').fold<int>(0, (m, l) => l.length > m ? l.length : m);
    final width = painter.width * (longest + 2);
    painter.dispose();
    return [
      // The Settings tab hides secret values; this one can't.
      if (_hasSecrets)
        const Notice(status: Status.warning, icon: Icons.visibility_outlined, message: 'Secret values, like passwords, show in plain text here.'),
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, _hasSecrets ? Space.sm : Space.lg, gutter, 0),
        child: ListenableBuilder(
          listenable: _yamlFocus,
          builder: (context, _) => InputDecorator(
            isFocused: _yamlFocus.hasFocus,
            decoration: InputDecoration(
              labelText: 'Compose file',
              floatingLabelBehavior: FloatingLabelBehavior.always,
              helperText:
                  'Everything about the app, including what the settings don’t show. Keep “name: ${_form?.appName ?? widget.app.id}”.'
                  ' Write a \$ in a value as \$\$.',
              helperMaxLines: 3,
              errorText: _yamlError,
              errorMaxLines: 6,
            ),
            child: LayoutBuilder(
              builder: (context, box) => SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                child: SizedBox(
                  width: width > box.maxWidth ? width : box.maxWidth,
                  child: TextField(
                    key: const ValueKey('compose-yaml'),
                    controller: _yaml,
                    focusNode: _yamlFocus,
                    minLines: 16,
                    maxLines: null,
                    keyboardType: TextInputType.multiline,
                    autocorrect: false,
                    enableSuggestions: false,
                    style: mono,
                    // The decorator around it draws the box; the theme's own
                    // borders and fill would draw a second one inside.
                    decoration: const InputDecoration(
                      isCollapsed: true,
                      filled: false,
                      border: InputBorder.none,
                      enabledBorder: InputBorder.none,
                      focusedBorder: InputBorder.none,
                      errorBorder: InputBorder.none,
                      focusedErrorBorder: InputBorder.none,
                      disabledBorder: InputBorder.none,
                    ),
                    onChanged: (_) => setState(() => _yamlError = validateComposeText(_yaml.text, _form?.appName ?? widget.app.id)),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    ];
  }

  /// Whether any variable holds a secret, which the compose file shows.
  bool get _hasSecrets => _form?.services.any((s) => s.envs.any((e) => isSecretName(e.key) && e.value.isNotEmpty)) ?? false;

  // --- The form -------------------------------------------------------------

  List<Widget> _formChildren(BuildContext context) {
    final form = _form!;
    return [
      Notice(
        status: Status.info,
        message:
            'Saving recreates $_title with these settings. It restarts, and a new image or tag is downloaded first. '
            'Its data folders are kept.',
      ),
      const SectionHeader(title: 'General'),
      _text(
        id: 'title',
        label: 'Name',
        value: form.title,
        helper: 'What Apps and the dashboard call it',
        validator: validateTitle,
        onChanged: (v) => form.title = v,
        action: TextInputAction.next,
      ),
      _iconField(context, form),
      ..._webUi(context, form),
      for (final s in form.services) ..._service(context, form, s),
      const SectionHeader(title: 'Advanced'),
      ListTile(
        leading: const Icon(Icons.code),
        title: const Text('Edit the compose file'),
        subtitle: const Text('For settings not shown here: labels, health checks, other services’ links'),
        trailing: const Icon(Icons.chevron_right),
        onTap: () => _switchMode(_Mode.yaml),
      ),
    ];
  }

  Widget _pad(Widget child, {double top = Space.md}) {
    final gutter = Space.gutter(context);
    return Padding(padding: EdgeInsets.fromLTRB(gutter, top, gutter, 0), child: child);
  }

  /// A text field for one form value. [id] keys it, so it keeps its state
  /// while rows around it come and go.
  Widget _text({
    required String id,
    required String label,
    required String value,
    required void Function(String) onChanged,
    String? helper,
    String? hint,
    String? Function(String)? validator,
    TextInputType? keyboard,
    TextInputAction? action,
    bool mono = false,
    bool obscure = false,
    Widget? suffix,
    Widget? prefix,
    bool padded = true,
    bool floatLabel = false,
    TextEditingController? controller,
  }) {
    final theme = Theme.of(context);
    final slot = '$_generation/$id';
    final field = TextFormField(
      key: _fields.putIfAbsent(slot, () => GlobalKey<FormFieldState<String>>(debugLabel: slot)),
      focusNode: _focus.putIfAbsent(slot, () => FocusNode(debugLabel: slot)),
      controller: controller,
      initialValue: controller == null ? value : null,
      keyboardType: keyboard,
      textInputAction: action,
      autocorrect: false,
      enableSuggestions: !mono && !obscure,
      obscureText: obscure,
      style: mono ? DesignTokens.of(context).mono(theme.textTheme.bodyLarge) : null,
      autovalidateMode: AutovalidateMode.onUserInteraction,
      validator: validator == null ? null : (v) => validator(v ?? ''),
      onChanged: (v) => _edit(() => onChanged(v)),
      decoration: InputDecoration(
        labelText: label,
        helperText: helper,
        helperMaxLines: 3,
        errorMaxLines: 3,
        hintText: hint,
        // A hint that says what blank means ("No limit") shows at rest.
        floatingLabelBehavior: floatLabel ? FloatingLabelBehavior.always : null,
        suffixIcon: suffix,
        prefixIcon: prefix,
      ),
    );
    return padded ? _pad(field) : field;
  }

  Widget _subhead(String text, {String? caption}) {
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.lg, gutter, 0),
      child: Semantics(
        header: true,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Below the service headings: a part of one service.
            Text(text, style: theme.textTheme.labelLarge?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
            if (caption != null) ...[
              const SizedBox(height: 2),
              Text(caption, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant)),
            ],
          ],
        ),
      ),
    );
  }

  Widget _addButton(String label, VoidCallback onPressed) {
    final gutter = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter - Space.md, Space.xs, gutter, 0),
      child: Align(
        alignment: AlignmentDirectional.centerStart,
        child: TextButton.icon(onPressed: onPressed, icon: const Icon(Icons.add), label: Text(label)),
      ),
    );
  }

  /// One entry of a list (a port, a folder, a variable) as a panel with its
  /// remove button.
  Widget _rowPanel({required Key key, required String removeTooltip, required VoidCallback onRemove, required List<Widget> children}) {
    final tokens = DesignTokens.of(context);
    final gutter = Space.gutter(context);
    return Padding(
      key: key,
      padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, 0),
      child: Material(
        color: tokens.cardColor,
        shape: tokens.cardShape(tokens.radii.md),
        child: Padding(
          // Room above for the outlined fields' floating labels.
          padding: const EdgeInsets.fromLTRB(Space.md, Space.md, Space.xs, Space.md),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: children),
              ),
              IconButton(tooltip: removeTooltip, icon: const Icon(Icons.delete_outline), onPressed: onRemove),
            ],
          ),
        ),
      ),
    );
  }

  Widget _iconField(BuildContext context, ComposeForm form) {
    final tokens = DesignTokens.of(context);
    final gutter = Space.gutter(context);
    return Padding(
      padding: EdgeInsets.fromLTRB(gutter, Space.md, gutter, 0),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Padding(
            padding: const EdgeInsets.only(top: Space.xs),
            child: ExcludeSemantics(
              child: NivaroAppIcon(
                key: ValueKey('icon-preview/${form.icon.trim()}'),
                iconUrl: validateIconUrl(form.icon) == null ? form.icon.trim() : '',
                name: _title,
                size: 48,
                radius: tokens.radii.md,
              ),
            ),
          ),
          const SizedBox(width: Space.md),
          Expanded(
            child: _text(
              id: 'icon',
              label: 'Icon address',
              value: form.icon,
              hint: 'https://…/icon.png',
              helper: 'A PNG or SVG on the web. The preview updates as you type.',
              keyboard: TextInputType.url,
              validator: validateIconUrl,
              onChanged: (v) => form.icon = v,
              padded: false,
            ),
          ),
        ],
      ),
    );
  }

  List<Widget> _webUi(BuildContext context, ComposeForm form) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final serverHost = Uri.tryParse(ApiClient.instance.baseUrl)?.host ?? '';
    final ports = form.publishedPorts;
    final link = form.link(serverHost);
    final gutter = Space.gutter(context);
    final testable = link != null && !form.problems().any((p) => p.startsWith('Web UI'));
    final url = Semantics(
      label: link == null ? 'No link' : 'Link: $link',
      child: ExcludeSemantics(
        child: Text(
          link?.toString() ?? 'No link: add a port or a host',
          style: link == null ? theme.textTheme.bodyMedium?.copyWith(color: scheme.onSurfaceVariant) : DesignTokens.of(context).mono(theme.textTheme.bodyMedium),
        ),
      ),
    );
    final test = TextButton.icon(onPressed: testable ? () => openAppUrl(context, link) : null, icon: const Icon(Icons.open_in_new), label: const Text('Test'));
    // Large text: the button goes under the link rather than squeezing it.
    final stacked = MediaQuery.textScalerOf(context).scale(10) > 13;
    return [
      const SectionHeader(title: 'Web UI link'),
      // The link first, so it stays in view while its parts are edited.
      Padding(
        padding: EdgeInsets.fromLTRB(gutter, 0, gutter, 0),
        child: stacked
            ? Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Icon(Icons.link, color: scheme.onSurfaceVariant),
                      const SizedBox(width: Space.md),
                      Expanded(child: url),
                    ],
                  ),
                  Align(alignment: AlignmentDirectional.centerEnd, child: test),
                ],
              )
            : Row(
                children: [
                  Icon(Icons.link, color: scheme.onSurfaceVariant),
                  const SizedBox(width: Space.md),
                  Expanded(child: url),
                  const SizedBox(width: Space.sm),
                  test,
                ],
              ),
      ),
      _pad(
        SegmentedButton<String>(
          segments: const [
            ButtonSegment(value: 'http', label: Text('http://')),
            ButtonSegment(value: 'https', label: Text('https://')),
          ],
          selected: {form.scheme == 'https' ? 'https' : 'http'},
          onSelectionChanged: (s) => _edit(() => form.scheme = s.first),
        ),
        top: Space.sm,
      ),
      _text(
        id: 'hostname',
        label: 'Host',
        value: form.hostname,
        hint: serverHost,
        floatLabel: serverHost.isNotEmpty,
        helper: serverHost.isEmpty ? 'Leave blank for this server' : 'Leave blank for this server ($serverHost)',
        keyboard: TextInputType.url,
        validator: validateHost,
        onChanged: (v) => form.hostname = v,
        action: TextInputAction.next,
      ),
      _text(
        id: 'port',
        label: 'Port',
        value: form.port,
        controller: _port,
        helper: ports.isEmpty ? 'The server port the app answers on' : 'Published: ${ports.join(', ')}',
        keyboard: TextInputType.number,
        validator: validateWebUiPort,
        onChanged: (v) => form.port = v,
        action: TextInputAction.next,
        suffix: ports.isEmpty
            ? null
            : PopupMenuButton<String>(
                tooltip: 'Pick a published port',
                icon: const Icon(Icons.arrow_drop_down),
                onSelected: (p) => _edit(() {
                  form.port = p;
                  _port.text = p;
                }),
                itemBuilder: (_) => [for (final p in ports) CheckedPopupMenuItem(value: p, checked: p == form.port.trim(), child: Text(p))],
              ),
      ),
      _text(
        id: 'index',
        label: 'Path',
        value: form.index,
        hint: '/',
        helper: 'Optional, like /web or /admin',
        keyboard: TextInputType.url,
        validator: validatePath,
        onChanged: (v) => form.index = v,
      ),
    ];
  }

  static const _restartLabels = {'': 'Not set (never)', 'unless-stopped': 'Unless stopped', 'always': 'Always', 'on-failure': 'On failure', 'no': 'Never'};

  static const _networkLabels = {'': 'The app’s own network', 'bridge': 'Bridge', 'host': 'Host (the server’s network)', 'none': 'None'};

  List<Widget> _service(BuildContext context, ComposeForm form, ServiceForm s) {
    final many = form.services.length > 1;
    final key = s.name;
    final restart = {..._restartLabels, if (!_restartLabels.containsKey(s.restart)) s.restart: s.restart};
    final networks = {..._networkLabels, if (!_networkLabels.containsKey(s.networkMode)) s.networkMode: s.networkMode};
    return [
      _serviceHeader(many ? (s.name == form.mainService ? 'Service: ${s.name} (main)' : 'Service: ${s.name}') : 'Container', s),
      _text(
        id: '$key/repo',
        label: 'Image',
        value: s.repo,
        hint: 'linuxserver/jellyfin',
        mono: true,
        keyboard: TextInputType.url,
        validator: (v) => !s.hadImage && v.trim().isEmpty && s.tag.trim().isEmpty ? null : validateImageRepo(v),
        onChanged: (v) => s.repo = v,
        action: TextInputAction.next,
      ),
      _text(
        id: '$key/tag',
        label: 'Tag',
        value: s.tag,
        hint: 'latest',
        mono: true,
        helper: s.digest != null
            ? 'Pinned to one exact build. A new image or tag removes the pin.'
            : 'Saving downloads this tag if the server doesn’t have it. “latest” follows new releases.',
        validator: validateTag,
        onChanged: (v) => s.tag = v,
      ),
      _pad(
        DropdownButtonFormField<String>(
          key: ValueKey('$_generation/$key/restart'),
          initialValue: s.restart,
          decoration: const InputDecoration(labelText: 'Restart', helperText: 'When Docker starts it again by itself', helperMaxLines: 3),
          items: [for (final e in restart.entries) DropdownMenuItem(value: e.key, child: Text(e.value))],
          onChanged: (v) => _edit(() => s.restart = v ?? ''),
        ),
      ),
      _pad(
        DropdownButtonFormField<String>(
          key: ValueKey('$_generation/$key/network'),
          initialValue: s.networkMode,
          isExpanded: true,
          decoration: InputDecoration(
            labelText: 'Network',
            helperText: s.networkMode == 'host' ? 'Uses the server’s ports directly: the port mappings below are ignored.' : null,
            helperMaxLines: 3,
          ),
          items: [
            for (final e in networks.entries)
              DropdownMenuItem(
                value: e.key,
                child: Text(e.value, overflow: TextOverflow.ellipsis),
              ),
          ],
          onChanged: (v) => _edit(() => s.networkMode = v ?? ''),
        ),
      ),
      ..._ports(s),
      ..._volumes(s),
      ..._envs(s),
      ..._devices(s),
      _subhead('Limits and access'),
      Padding(
        padding: EdgeInsets.fromLTRB(Space.gutter(context), Space.sm, Space.gutter(context), 0),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: _text(
                id: '$key/cpus',
                label: 'CPU cores',
                value: s.cpus,
                hint: 'No limit',
                floatLabel: true,
                keyboard: const TextInputType.numberWithOptions(decimal: true),
                validator: validateCpus,
                onChanged: (v) => s.cpus = v,
                padded: false,
              ),
            ),
            const SizedBox(width: Space.md),
            Expanded(
              child: _text(
                id: '$key/memory',
                label: 'Memory (MB)',
                value: s.memoryMb,
                hint: 'No limit',
                floatLabel: true,
                keyboard: TextInputType.number,
                validator: validateMemory,
                onChanged: (v) => s.memoryMb = v,
                padded: false,
              ),
            ),
          ],
        ),
      ),
      _text(
        id: '$key/caps',
        label: 'Capabilities',
        value: s.caps,
        hint: 'NET_ADMIN, SYS_TIME',
        helper: 'Extra Linux capabilities, separated by commas',
        mono: true,
        validator: validateCaps,
        onChanged: (v) => s.caps = v,
      ),
      _text(
        id: '$key/command',
        label: 'Command',
        value: s.command,
        hint: 'The image’s own',
        helper: 'Replaces the image’s start command. Leave blank to keep it.',
        mono: true,
        onChanged: (v) => s.command = v,
      ),
      Padding(
        padding: const EdgeInsets.only(top: Space.sm),
        child: SwitchListTile(
          value: s.privileged,
          onChanged: (v) => _edit(() => s.privileged = v),
          title: const Text('Privileged'),
          subtitle: const Text('Full access to the server’s devices and kernel. Only for apps that need it.'),
        ),
      ),
    ];
  }

  /// Where a service starts: a rule, its heading and its image, so it
  /// stands out from the parts under it (Ports, Folders, …).
  Widget _serviceHeader(String title, ServiceForm s) {
    final tokens = DesignTokens.of(context);
    final theme = Theme.of(context);
    final gutter = Space.gutter(context);
    final image = s.image.trim();
    return Padding(
      padding: const EdgeInsets.only(top: Space.lg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Divider(height: 1, indent: gutter, endIndent: gutter),
          Padding(
            padding: EdgeInsets.fromLTRB(gutter, Space.lg, gutter, 0),
            child: Semantics(
              header: true,
              child: Text(title, style: tokens.sectionLabel),
            ),
          ),
          if (image.isNotEmpty)
            Padding(
              padding: EdgeInsets.fromLTRB(gutter, 2, gutter, 0),
              child: Text(
                image,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: tokens.mono(theme.textTheme.bodySmall).copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
            ),
        ],
      ),
    );
  }

  List<Widget> _ports(ServiceForm s) => [
    _subhead('Ports', caption: 'A port on the server, and the app’s port inside its container'),
    for (final p in s.ports)
      _rowPanel(
        key: ObjectKey(p),
        removeTooltip: 'Remove port ${p.published.isEmpty ? p.target : p.published}',
        onRemove: () => _edit(() {
          s.ports.remove(p);
          _portEdited();
        }),
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: _text(
                  id: fieldId('published', service: s, row: p),
                  label: 'Server',
                  value: p.published,
                  keyboard: TextInputType.number,
                  validator: (v) => p.changed ? validatePort(v, required: false) : null,
                  onChanged: (v) {
                    p.published = v;
                    _portEdited();
                  },
                  padded: false,
                ),
              ),
              const SizedBox(width: Space.sm),
              Expanded(
                child: _text(
                  id: fieldId('target', service: s, row: p),
                  label: 'App',
                  value: p.target,
                  keyboard: TextInputType.number,
                  validator: (v) => p.changed && (v.trim().isNotEmpty || p.published.trim().isNotEmpty) ? validatePort(v, required: true) : null,
                  onChanged: (v) => p.target = v,
                  padded: false,
                ),
              ),
            ],
          ),
          const SizedBox(height: Space.sm),
          SegmentedButton<String>(
            segments: const [
              ButtonSegment(value: 'tcp', label: Text('TCP')),
              ButtonSegment(value: 'udp', label: Text('UDP')),
            ],
            selected: {p.protocol == 'udp' ? 'udp' : 'tcp'},
            showSelectedIcon: false,
            onSelectionChanged: (v) => _edit(() => p.protocol = v.first),
          ),
        ],
      ),
    _addButton('Add port', () => _edit(() => s.ports.add(PortRow()))),
  ];

  List<Widget> _volumes(ServiceForm s) => [
    _subhead('Folders', caption: 'A folder on the server, and where the app sees it'),
    for (final v in s.volumes)
      _rowPanel(
        key: ObjectKey(v),
        removeTooltip: 'Remove folder ${v.target}',
        onRemove: () => _edit(() => s.volumes.remove(v)),
        children: [
          // A tmpfs lives in memory: there is no folder on the server.
          if (v.type != 'tmpfs') ...[
            _text(
              id: fieldId('source', service: s, row: v),
              label: v.type == 'volume' ? 'Docker volume' : 'On the server',
              value: v.source,
              hint: v.type == 'volume' ? (v.raw == null ? 'Volume name' : 'Anonymous') : '/DATA/AppData/${widget.app.id}/config',
              mono: true,
              validator: (_) => v.target.trim().isEmpty && v.source.trim().isEmpty ? null : v.sourceProblem(),
              onChanged: (x) => v.source = x,
              padded: false,
            ),
            const SizedBox(height: Space.sm),
          ],
          _text(
            id: fieldId('target', service: s, row: v),
            label: v.type == 'tmpfs' ? 'In the app (in memory)' : 'In the app',
            value: v.target,
            hint: '/config',
            mono: true,
            validator: (x) => !v.changed || (x.trim().isEmpty && v.source.trim().isEmpty) ? null : validateContainerPath(x),
            onChanged: (x) => v.target = x,
            padded: false,
          ),
          const SizedBox(height: Space.xs),
          _readOnlySwitch(v),
        ],
      ),
    _addButton('Add folder', () => _edit(() => s.volumes.add(VolumeRow(source: '/DATA/AppData/${widget.app.id}/')))),
  ];

  /// Read-only for a folder: the label on the fields' left edge and a
  /// switch like Privileged's, the whole row one tap target.
  Widget _readOnlySwitch(VolumeRow v) {
    final theme = Theme.of(context);
    return MergeSemantics(
      child: InkWell(
        borderRadius: BorderRadius.circular(DesignTokens.of(context).radii.sm),
        onTap: () => _edit(() => v.readOnly = !v.readOnly),
        child: ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 48),
          child: Row(
            children: [
              Expanded(child: Text('Read-only', style: theme.textTheme.bodyLarge)),
              Switch(value: v.readOnly, onChanged: (x) => _edit(() => v.readOnly = x)),
            ],
          ),
        ),
      ),
    );
  }

  List<Widget> _envs(ServiceForm s) => [
    _subhead('Environment variables', caption: 'Settings the app reads when it starts. Values marked secret stay hidden until you tap the eye.'),
    for (final e in s.envs)
      _rowPanel(
        key: ObjectKey(e),
        removeTooltip: 'Remove ${e.key.isEmpty ? 'variable' : e.key}',
        onRemove: () => _edit(() => s.envs.remove(e)),
        children: [
          _text(
            id: fieldId('key', service: s, row: e),
            label: 'Name',
            value: e.key,
            mono: true,
            validator: (v) => !e.changed || (v.trim().isEmpty && e.value.isEmpty) ? null : validateEnvKey(v),
            onChanged: (v) => e.key = v,
            padded: false,
          ),
          const SizedBox(height: Space.sm),
          () {
            final secret = isSecretName(e.key);
            final shown = !secret || _revealed.contains(e);
            return _text(
              id: fieldId('value', service: s, row: e),
              label: 'Value',
              value: e.value,
              mono: true,
              helper: s.envHelp[e.key.trim()],
              obscure: !shown,
              onChanged: (v) => e.value = v,
              padded: false,
              suffix: secret
                  ? IconButton(
                      tooltip: shown ? 'Hide value' : 'Show value',
                      icon: Icon(shown ? Icons.visibility_off_outlined : Icons.visibility_outlined),
                      onPressed: () => setState(() => shown ? _revealed.remove(e) : _revealed.add(e)),
                    )
                  : null,
            );
          }(),
        ],
      ),
    _addButton('Add variable', () => _edit(() => s.envs.add(EnvRow()))),
  ];

  List<Widget> _devices(ServiceForm s) => [
    _subhead('Devices', caption: 'Hardware the app can use, like a GPU (/dev/dri)'),
    for (final d in s.devices)
      _rowPanel(
        key: ObjectKey(d),
        removeTooltip: 'Remove device ${d.host}',
        onRemove: () => _edit(() => s.devices.remove(d)),
        children: [
          _text(
            id: fieldId('host', service: s, row: d),
            label: 'On the server',
            value: d.host,
            hint: '/dev/dri',
            mono: true,
            validator: (v) => !d.changed || (v.trim().isEmpty && d.container.trim().isEmpty) ? null : validateDevice(v),
            onChanged: (v) => d.host = v,
            padded: false,
          ),
          const SizedBox(height: Space.sm),
          _text(
            id: fieldId('container', service: s, row: d),
            label: 'In the app',
            value: d.container,
            hint: 'Same as on the server',
            mono: true,
            onChanged: (v) => d.container = v,
            padded: false,
          ),
        ],
      ),
    _addButton('Add device', () => _edit(() => s.devices.add(DeviceRow(host: '/dev/')))),
  ];
}

String _reason(Object e) {
  final s = e is ApiException ? e.message : e.toString().replaceFirst('Exception: ', '');
  return s.endsWith('.') ? s : '$s.';
}

/// What went wrong saving, in words: the server's port check names the
/// ports another app already uses.
String saveErrorText(Object e) {
  if (e is ApiException) {
    final data = e.data;
    final inUse = data is Map ? data['ports_in_use'] : null;
    if (inUse is Map) {
      final ports = [
        for (final p in (inUse['TCP'] is List ? inUse['TCP'] as List : const [])) '$p',
        for (final p in (inUse['UDP'] is List ? inUse['UDP'] as List : const [])) '$p/UDP',
      ];
      if (ports.isNotEmpty) {
        return ports.length == 1
            ? 'Server port ${ports.first} is already used by something else. Pick another server port.'
            : 'Server ports ${ports.join(', ')} are already used by something else. Pick other server ports.';
      }
    }
  }
  return _reason(e);
}
