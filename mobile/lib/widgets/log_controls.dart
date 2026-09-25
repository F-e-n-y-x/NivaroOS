import 'package:flutter/material.dart';

import '../ui/ui.dart';

/// Which entries a log view shows.
enum LogFilter { all, problems, errors }

/// The search field and level chips every log view shares (the server log
/// and an app's container log), so the two read as one tool: the same
/// search bar, the same three chips with a check on the selected one.
class LogFilterBar extends StatefulWidget {
  const LogFilterBar({super.key, required this.search, required this.filter, required this.onFilter, this.hint = 'Search the log'});

  final TextEditingController search;
  final LogFilter filter;
  final ValueChanged<LogFilter> onFilter;
  final String hint;

  static const _labels = [(LogFilter.all, 'All'), (LogFilter.problems, 'Warnings and errors'), (LogFilter.errors, 'Errors')];

  @override
  State<LogFilterBar> createState() => _LogFilterBarState();
}

class _LogFilterBarState extends State<LogFilterBar> {
  final _chips = ScrollController();

  @override
  void dispose() {
    _chips.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final search = widget.search;
    final filter = widget.filter;
    final onFilter = widget.onFilter;
    final hint = widget.hint;
    final scheme = Theme.of(context).colorScheme;
    final gutter = Space.gutter(context);
    return ColoredBox(
      color: scheme.surface,
      child: Padding(
        padding: EdgeInsets.fromLTRB(gutter, Space.sm, gutter, Space.sm),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: [
            ListenableBuilder(
              listenable: search,
              builder: (context, _) => SearchBar(
                controller: search,
                hintText: hint,
                leading: const Icon(Icons.search),
                elevation: const WidgetStatePropertyAll(0),
                constraints: const BoxConstraints(minHeight: 48),
                trailing: [
                  if (search.text.isNotEmpty) IconButton(onPressed: search.clear, tooltip: 'Clear search', icon: const Icon(Icons.close)),
                ],
              ),
            ),
            const SizedBox(height: Space.sm),
            // Chips rather than a segmented button: "Warnings and errors"
            // would break in two inside a segment at large text sizes. One
            // row that scrolls, so at large text the chips don't take
            // three lines from the log.
            SizedBox(
              height: MediaQuery.textScalerOf(context).scale(20) + 28,
              child: FadingEdges(
                controller: _chips,
                child: ListView(
                  controller: _chips,
                  scrollDirection: Axis.horizontal,
                  children: [
                    for (final (i, (f, label)) in LogFilterBar._labels.indexed) ...[
                      if (i > 0) const SizedBox(width: Space.sm),
                      Center(child: ChoiceChip(label: Text(label), selected: filter == f, onSelected: (_) => onFilter(f))),
                    ],
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
