import 'package:flutter/foundation.dart';

import 'storage_service.dart';

/// How often Home's live widgets - processor, memory, network, graphics,
/// running VMs - and their detail pages ask the server for a new reading
/// (Settings > Home > Refresh widgets). [manual] polls nothing: the
/// numbers move on pull-to-refresh only.
///
/// Drive usage keeps its own slower pace ([drivesEvery]) and the VM
/// console preview never refreshes faster than [previewFloor], whatever
/// is picked here.
enum WidgetRefresh {
  s2(Duration(seconds: 2), '2 seconds'),
  s4(Duration(seconds: 4), '4 seconds'),
  s10(Duration(seconds: 10), '10 seconds'),
  s30(Duration(seconds: 30), '30 seconds'),
  m1(Duration(minutes: 1), '1 minute'),
  manual(null, 'Only when I pull to refresh');

  const WidgetRefresh(this.every, this.label);

  /// The poll interval; null for [manual].
  final Duration? every;

  /// The choice as the picker lists it.
  final String label;

  static const defaultValue = s4;

  /// Drives change slowly: never read more often than this.
  static const drivesEvery = Duration(seconds: 30);

  /// A screenshot is heavier than a reading: the console preview and the
  /// VM list it follows refresh no faster than this.
  static const previewFloor = Duration(seconds: 5);

  /// The settings row's summary: "Every 4 seconds", "Every minute",
  /// "Only when I pull to refresh".
  String get summary => switch (this) {
        manual => label,
        m1 => 'Every minute',
        _ => 'Every $label',
      };

  /// The console preview's (and the VM list's) interval: the chosen one,
  /// but at least [previewFloor]; null for [manual].
  Duration? get previewEvery {
    final e = every;
    if (e == null) return null;
    return e < previewFloor ? previewFloor : e;
  }
}

/// The phone's [WidgetRefresh] choice, persisted in [StorageService].
/// Home listens to it and re-times its polling at once; Settings writes it.
class WidgetRefreshController extends ValueNotifier<WidgetRefresh> {
  /// Use [instance]; separate controllers exist only for tests.
  WidgetRefreshController([super.value = WidgetRefresh.defaultValue]);
  static final WidgetRefreshController instance = WidgetRefreshController();

  /// Reads the saved choice. Call once after `StorageService.init()`;
  /// anything unreadable keeps the default.
  Future<void> load() async {
    try {
      value = WidgetRefresh.values.asNameMap()[await StorageService.instance.getWidgetRefresh()] ?? WidgetRefresh.defaultValue;
    } catch (e) {
      debugPrint('[refresh] Could not read the refresh setting: $e');
    }
  }

  /// Applies [next] at once and saves it. If saving fails the choice still
  /// holds for this session.
  Future<void> set(WidgetRefresh next) async {
    if (next == value) return;
    value = next;
    try {
      await StorageService.instance.setWidgetRefresh(next.name);
    } catch (e) {
      debugPrint('[refresh] Could not save the refresh setting: $e');
    }
  }
}
