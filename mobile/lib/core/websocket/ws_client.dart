import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../protocol/packet.dart';
import '../storage/secure_storage.dart' as storage;
import '../api/api_client.dart';

/// WebSocket connection state.
enum WsState { disconnected, connecting, authenticating, connected }

final wsClientProvider = Provider<WsClient>((ref) {
  final api = ref.watch(apiClientProvider);
  return WsClient(
    serverUrl: const String.fromEnvironment(
      'CCMUX_WS_URL',
      defaultValue: 'ws://localhost:8080',
    ),
    api: api,
  );
});

class WsClient {
  final String serverUrl;
  final ApiClient api;

  WsClient({required this.serverUrl, required this.api});

  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _sub;
  WsState _state = WsState.disconnected;

  final _packetController = StreamController<Packet>.broadcast();
  final _stateController  = StreamController<WsState>.broadcast();

  Stream<Packet>   get packets => _packetController.stream;
  Stream<WsState>  get states  => _stateController.stream;
  WsState          get state   => _state;

  void _setState(WsState s) {
    _state = s;
    _stateController.add(s);
  }

  /// Connect and authenticate. Safe to call multiple times (no-op if connected).
  Future<void> connect() async {
    if (_state == WsState.connected || _state == WsState.connecting) return;
    _setState(WsState.connecting);

    final tokens = await storage.loadTokens();
    String? accessToken = tokens.accessToken;

    // If the stored access token is stale, refresh it first.
    if (!storage.isAccessTokenValid(accessToken) && tokens.refreshToken != null) {
      try {
        accessToken = await api.refresh(tokens.refreshToken!);
        await storage.saveTokens(
          accessToken:  accessToken,
          refreshToken: tokens.refreshToken!,
          userID:       tokens.userID ?? '',
        );
      } catch (_) {
        _setState(WsState.disconnected);
        return;
      }
    }
    if (accessToken == null) {
      _setState(WsState.disconnected);
      return;
    }

    try {
      _channel = WebSocketChannel.connect(Uri.parse('$serverUrl/ws/client'));
      await _channel!.ready;
    } catch (_) {
      _setState(WsState.disconnected);
      return;
    }

    _setState(WsState.authenticating);

    // Send TypeAuth with JWT as binary payload.
    final authPkt = Packet(
      type: typeAuth,
      payload: Uint8List.fromList(utf8.encode(accessToken)),
    ).encode();
    _channel!.sink.add(authPkt);

    // Wait for TypeAuthOK / TypeAuthFail as the first incoming frame.
    final completer = Completer<bool>();

    _sub = _channel!.stream.listen(
      (msg) {
        if (msg is! Uint8List) return;
        final pkt = Packet.decode(msg);
        if (!completer.isCompleted) {
          completer.complete(pkt.type == typeAuthOK);
          return;
        }
        // Post-auth packets go to the broadcast stream.
        _packetController.add(pkt);
      },
      onDone: () {
        if (!completer.isCompleted) completer.complete(false);
        _setState(WsState.disconnected);
      },
      onError: (_) {
        if (!completer.isCompleted) completer.complete(false);
        _setState(WsState.disconnected);
      },
    );

    final ok = await completer.future.timeout(
      const Duration(seconds: 15),
      onTimeout: () => false,
    );

    if (ok) {
      _setState(WsState.connected);
    } else {
      disconnect();
    }
  }

  /// Send a pre-encoded packet binary to the server.
  void send(Uint8List data) {
    if (_state == WsState.connected) {
      _channel?.sink.add(data);
    }
  }

  /// Subscribe to a session's output stream, requesting scrollback from [fromOffset].
  void subscribe(String sessionId, {String fromOffset = ''}) {
    final payload = encodeSubscribePayload(sessionId, fromOffset: fromOffset);
    send(Packet(type: typeSubscribe, session: sessionId, payload: payload).encode());
  }

  /// Unsubscribe from a session.
  void unsubscribe(String sessionId) {
    send(Packet(type: typeUnsubscribe, session: sessionId).encode());
  }

  /// Send terminal input to a session.
  void sendInput(String sessionId, Uint8List data) {
    send(Packet(type: typeTerminalInput, session: sessionId, payload: data).encode());
  }

  /// Send a resize event to a session.
  void sendResize(String sessionId, int cols, int rows) {
    final payload = encodeResizePayload(cols, rows);
    send(Packet(type: typeResize, session: sessionId, payload: payload).encode());
  }

  void disconnect() {
    _sub?.cancel();
    _channel?.sink.close();
    _channel = null;
    _setState(WsState.disconnected);
  }

  void dispose() {
    disconnect();
    _packetController.close();
    _stateController.close();
  }
}
