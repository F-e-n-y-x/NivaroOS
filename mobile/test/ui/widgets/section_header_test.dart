import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:nivaroos_mobile/ui/theme/app_theme.dart';
import 'package:nivaroos_mobile/ui/theme/design_tokens.dart';
import 'package:nivaroos_mobile/ui/widgets/section_header.dart';

import '../pump.dart';

void main() {
  testWidgets("shows the title as a header in the direction's section style", (tester) async {
    final handle = tester.ensureSemantics();
    await pumpUi(tester, const SectionHeader(title: 'Storage'));
    final text = tester.widget<Text>(find.text('Storage'));
    expect(text.style, AppTheme.light().extension<DesignTokens>()!.sectionLabel);
    // Sentence case as written: no spaced-capital eyebrows.
    expect(text.data, 'Storage');
    expect(tester.getSemantics(find.text('Storage')), matchesSemantics(label: 'Storage', isHeader: true));
    expect(find.byType(TextButton), findsNothing);
    handle.dispose();
  });

  testWidgets('the action appears only with a callback, and fires', (tester) async {
    var taps = 0;
    await pumpUi(tester, const SectionHeader(title: 'Apps', actionLabel: 'See all'));
    expect(find.text('See all'), findsNothing);

    await pumpUi(tester, SectionHeader(title: 'Apps', actionLabel: 'See all', onAction: () => taps++));
    await tester.tap(find.text('See all'));
    expect(taps, 1);
  });
}
