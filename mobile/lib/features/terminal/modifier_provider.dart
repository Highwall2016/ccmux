import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Tracks which modifier key (Ctrl / Alt) is currently armed.
/// Exactly one can be armed at a time; arming one disarms the other.
enum ArmedModifier { none, ctrl, alt }

final modifierProvider = StateProvider<ArmedModifier>(
  (ref) => ArmedModifier.none,
);
