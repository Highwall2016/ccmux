import 'package:flutter/material.dart';

class CcmuxColors {
  static const bg        = Color(0xFF0D0D0F);
  static const bgDark    = Color(0xFF080809);
  static const bgToolbar = Color(0xFF0A0A0C);
  static const surface   = Color(0xFF16161A);
  static const surface2  = Color(0xFF1A1A1E);
  static const accent    = Color(0xFF3DDC84);
  static const text      = Color(0xFFFFFFFF);
  static const textSub   = Color(0xFFAAAAAA);
  static const textMuted = Color(0xFF888888);
  static const textDim   = Color(0xFF555555);
  static const textFaint = Color(0xFF444444);
  static const divider   = Color(0x0FFFFFFF);
  static const red       = Color(0xFFE06C75);
  static const blue      = Color(0xFF61AFEF);
  static const cyan      = Color(0xFF56B6C2);
  static const yellow    = Color(0xFFE5C07B);
  static const purple    = Color(0xFFC678DD);

  static const _palette = [accent, blue, yellow, purple, cyan, red];

  static Color forName(String name) {
    if (name.isEmpty) return accent;
    final hash = name.codeUnits.fold(0, (a, b) => a + b);
    return _palette[hash % _palette.length];
  }

  static Color statusDot(String status, int? exitCode) {
    if (status == 'active') return accent;
    if (exitCode == 0) return cyan;
    return red;
  }
}

ThemeData buildCcmuxTheme() => ThemeData.dark(useMaterial3: true).copyWith(
  scaffoldBackgroundColor: CcmuxColors.bg,
  colorScheme: const ColorScheme.dark(
    primary: CcmuxColors.accent,
    onPrimary: Colors.black,
    surface: CcmuxColors.surface,
    onSurface: CcmuxColors.text,
    error: CcmuxColors.red,
  ),
  appBarTheme: const AppBarTheme(
    backgroundColor: CcmuxColors.bg,
    elevation: 0,
    scrolledUnderElevation: 0,
    foregroundColor: CcmuxColors.text,
  ),
  dividerColor: CcmuxColors.divider,
  bottomSheetTheme: const BottomSheetThemeData(
    backgroundColor: CcmuxColors.surface,
    shape: RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
    ),
    showDragHandle: false,
  ),
);
