import 'package:flutter/material.dart';

import '../theme/spacing.dart';

/// An on/off entry in a popup menu. The label lines up with the plain
/// entries around it and the check sits at the end, as in Android's own
/// menus. (Material's CheckedPopupMenuItem keeps an empty check slot in
/// front of the label, which pushes it out of line when it's off.)
PopupMenuItem<T> checkMenuItem<T>({required T value, required bool checked, required String label}) => PopupMenuItem<T>(
      value: value,
      child: Semantics(
        checked: checked,
        child: Row(
          children: [
            Expanded(child: Text(label)),
            const SizedBox(width: Space.md),
            // The slot keeps its width when off, so the menu doesn't change
            // size when an entry is toggled.
            SizedBox(width: 24, child: checked ? const Icon(Icons.check, size: 20) : null),
          ],
        ),
      ),
    );
