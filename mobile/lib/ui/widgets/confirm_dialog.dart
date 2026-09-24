import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Confirmation dialogs. Both return true only when the user confirms;
/// dismissing (back, tapping outside) counts as Cancel.
///
///   if (await ConfirmDialog.destructive(context,
///       title: 'Delete “mint”?',
///       message: 'The VM and its disk are deleted.',
///       confirmLabel: 'Delete')) { ... }
abstract final class ConfirmDialog {
  /// For something that cannot be undone. The title names the thing, the
  /// message says what goes, and "This can't be undone." is added unless
  /// [permanent] is false (a reboot, for example, is disruptive but not
  /// permanent). The confirm button is in the error colour and Cancel has
  /// focus, so a stray Enter or tap on the default cancels. The only
  /// haptic is on the confirm itself.
  static Future<bool> destructive(
    BuildContext context, {
    required String title,
    required String message,
    required String confirmLabel,
    bool permanent = true,
  }) {
    return _show(
      context,
      title: title,
      message: permanent ? _withPermanence(message) : message,
      confirmLabel: confirmLabel,
      destructive: true,
    );
  }

  // Adds the permanence sentence, closing the message's own sentence first
  // if it doesn't end in punctuation.
  static String _withPermanence(String message) {
    final text = message.trimRight();
    final closed = RegExp(r'[.!?…]$').hasMatch(text) ? text : '$text.';
    return "$closed This can't be undone.";
  }

  /// For a reversible action that still deserves a pause ("Sign out?").
  static Future<bool> confirm(
    BuildContext context, {
    required String title,
    required String message,
    required String confirmLabel,
  }) =>
      _show(context, title: title, message: message, confirmLabel: confirmLabel, destructive: false);

  static Future<bool> _show(
    BuildContext context, {
    required String title,
    required String message,
    required String confirmLabel,
    required bool destructive,
  }) async {
    final result = await showDialog<bool>(
      context: context,
      builder: (context) {
        final scheme = Theme.of(context).colorScheme;
        return AlertDialog(
          title: Text(title),
          content: Text(message),
          actions: [
            TextButton(
              autofocus: true,
              onPressed: () => Navigator.of(context).pop(false),
              child: const Text('Cancel'),
            ),
            if (destructive)
              FilledButton(
                style: FilledButton.styleFrom(backgroundColor: scheme.error, foregroundColor: scheme.onError),
                onPressed: () {
                  HapticFeedback.heavyImpact();
                  Navigator.of(context).pop(true);
                },
                child: Text(confirmLabel),
              )
            else
              TextButton(onPressed: () => Navigator.of(context).pop(true), child: Text(confirmLabel)),
          ],
        );
      },
    );
    return result ?? false;
  }
}
