import 'dart:async';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'ws_client.dart';

/// Manages automatic WebSocket reconnection with exponential backoff.
/// Also triggers an immediate reconnect when network connectivity is restored.
class WsReconnectManager {
  final WsClient client;

  WsReconnectManager({required this.client});

  Timer? _backoffTimer;
  StreamSubscription<List<ConnectivityResult>>? _connectivitySub;
  int _attempt = 0;
  bool _active = false;

  /// Start managing reconnection. Call once at app startup.
  void start() {
    _active = true;
    _scheduleConnect();

    _connectivitySub = Connectivity().onConnectivityChanged.listen((results) {
      final online = results.any((r) =>
          r == ConnectivityResult.wifi ||
          r == ConnectivityResult.mobile ||
          r == ConnectivityResult.ethernet);
      if (online && client.state == WsState.disconnected) {
        _backoffTimer?.cancel();
        _attempt = 0; // reset backoff on network restoration
        _scheduleConnect();
      }
    });

    // Re-check on state changes.
    client.states.listen((state) {
      if (!_active) return;
      if (state == WsState.disconnected) {
        _scheduleConnect();
      } else if (state == WsState.connected) {
        _attempt = 0;
        _backoffTimer?.cancel();
      }
    });
  }

  void stop() {
    _active = false;
    _backoffTimer?.cancel();
    _connectivitySub?.cancel();
  }

  void _scheduleConnect() {
    if (!_active || client.state != WsState.disconnected) return;
    final delay = _backoff();
    _backoffTimer?.cancel();
    _backoffTimer = Timer(delay, () async {
      if (_active && client.state == WsState.disconnected) {
        _attempt++;
        await client.connect();
      }
    });
  }

  Duration _backoff() {
    if (_attempt == 0) return const Duration(seconds: 1);
    final secs = (1 << _attempt).clamp(1, 60);
    return Duration(seconds: secs);
  }

  void dispose() {
    stop();
  }
}
