import 'package:flutter/material.dart';

/// The NivaroOS mark, for the first screens (discovery, sign-in), where
/// people should see whose app this is. Goes in the app bar's leading
/// slot; TalkBack reads "NivaroOS".
class BrandMark extends StatelessWidget {
  const BrandMark({super.key, this.size = 32});

  final double size;

  @override
  Widget build(BuildContext context) {
    final px = (size * MediaQuery.devicePixelRatioOf(context)).round();
    return Semantics(
      label: 'NivaroOS',
      image: true,
      child: Center(
        child: Image.asset('assets/icon/icon.png', width: size, height: size, cacheWidth: px, cacheHeight: px, excludeFromSemantics: true),
      ),
    );
  }
}
