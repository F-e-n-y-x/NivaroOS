import 'package:flutter/material.dart';

import '../theme/spacing.dart';
import '../theme/theme_controller.dart';

/// The "Theme" setting: shows the current choice and opens a sheet with
/// System default / Light / Dark. The change applies immediately and is
/// saved for the next launch.
class ThemeModeTile extends StatelessWidget {
  const ThemeModeTile({super.key, this.controller});

  /// Defaults to [ThemeController.instance]; tests pass their own.
  final ThemeController? controller;

  static const _icons = {
    ThemeMode.system: Icons.brightness_auto_outlined,
    ThemeMode.light: Icons.light_mode_outlined,
    ThemeMode.dark: Icons.dark_mode_outlined,
  };

  @override
  Widget build(BuildContext context) {
    final c = controller ?? ThemeController.instance;
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: c,
      builder: (context, mode, _) => ListTile(
        leading: Icon(_icons[mode]),
        title: const Text('Theme'),
        subtitle: Text(ThemeController.label(mode)),
        onTap: () => _pick(context, c),
      ),
    );
  }

  Future<void> _pick(BuildContext context, ThemeController c) async {
    final picked = await showModalBottomSheet<ThemeMode>(
      context: context,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: RadioGroup<ThemeMode>(
          groupValue: c.value,
          onChanged: (m) => Navigator.of(context).pop(m),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(Space.xl, 0, Space.xl, Space.sm),
                child: Text('Theme', style: Theme.of(context).textTheme.titleLarge),
              ),
              for (final m in ThemeMode.values)
                RadioListTile<ThemeMode>(
                  value: m,
                  title: Text(ThemeController.label(m)),
                  subtitle: m == ThemeMode.system ? const Text("Follows this phone's dark theme setting") : null,
                ),
              const SizedBox(height: Space.sm),
            ],
          ),
        ),
      ),
    );
    if (picked != null) await c.setMode(picked);
  }
}
