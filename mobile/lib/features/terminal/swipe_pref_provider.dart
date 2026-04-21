import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _kSwipeKey = 'tmux_swipe_enabled';

class SwipePrefNotifier extends AsyncNotifier<bool> {
  @override
  Future<bool> build() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getBool(_kSwipeKey) ?? false;
  }

  Future<void> toggle() async {
    final current = state.valueOrNull ?? false;
    final next = !current;
    state = AsyncValue.data(next);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_kSwipeKey, next);
  }
}

final swipePrefProvider =
    AsyncNotifierProvider<SwipePrefNotifier, bool>(() => SwipePrefNotifier());
