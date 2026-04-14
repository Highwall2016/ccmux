import 'dart:async';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:messagepack/messagepack.dart';
import 'package:xterm/xterm.dart';

import '../../core/protocol/packet.dart';
import '../../core/websocket/ws_client.dart';
import '../../core/websocket/ws_reconnect.dart';

/// Per-session state.
class TerminalSessionState {
  final String id;
  final Terminal terminal;
  final String status; // active | exited | killed
  final int? exitCode;
  final bool hasNewOutput;

  const TerminalSessionState({
    required this.id,
    required this.terminal,
    this.status = 'active',
    this.exitCode,
    this.hasNewOutput = false,
  });

  TerminalSessionState copyWith({
    String? status,
    int? exitCode,
    bool? hasNewOutput,
  }) =>
      TerminalSessionState(
        id:           id,
        terminal:     terminal,
        status:       status       ?? this.status,
        exitCode:     exitCode     ?? this.exitCode,
        hasNewOutput: hasNewOutput ?? this.hasNewOutput,
      );
}

class TerminalState {
  final Map<String, TerminalSessionState> sessions;
  final String? activeSessionId;

  const TerminalState({this.sessions = const {}, this.activeSessionId});

  TerminalState copyWith({
    Map<String, TerminalSessionState>? sessions,
    String? activeSessionId,
    bool clearActive = false,
  }) =>
      TerminalState(
        sessions:        sessions        ?? this.sessions,
        activeSessionId: clearActive ? null : (activeSessionId ?? this.activeSessionId),
      );

  TerminalSessionState? get activeSession =>
      activeSessionId != null ? sessions[activeSessionId] : null;
}

class TerminalNotifier extends AsyncNotifier<TerminalState> {
  StreamSubscription<Packet>? _packetSub;
  WsReconnectManager? _reconnect;

  @override
  Future<TerminalState> build() async {
    final ws = ref.watch(wsClientProvider);

    _reconnect = WsReconnectManager(client: ws);
    _reconnect!.start();

    _packetSub = ws.packets.listen(_onPacket);
    ref.onDispose(() {
      _packetSub?.cancel();
      _reconnect?.dispose();
    });

    return const TerminalState();
  }

  /// Subscribe to a session and create a [Terminal] for it.
  void openSession(String sessionId) {
    final current = state.valueOrNull ?? const TerminalState();
    if (current.sessions.containsKey(sessionId)) {
      state = AsyncValue.data(current.copyWith(activeSessionId: sessionId));
      return;
    }

    final terminal = Terminal(maxLines: 10000);
    final sessions = Map<String, TerminalSessionState>.from(current.sessions);
    sessions[sessionId] = TerminalSessionState(id: sessionId, terminal: terminal);

    state = AsyncValue.data(TerminalState(
      sessions:        sessions,
      activeSessionId: sessionId,
    ));

    // Subscribe to the session stream (with scrollback replay from beginning).
    ref.read(wsClientProvider).subscribe(sessionId);
  }

  void setActiveSession(String sessionId) {
    final current = state.valueOrNull ?? const TerminalState();
    if (!current.sessions.containsKey(sessionId)) return;

    // Clear new-output indicator when switching to this tab.
    final sessions = Map<String, TerminalSessionState>.from(current.sessions);
    final sess = sessions[sessionId]!;
    sessions[sessionId] = sess.copyWith(hasNewOutput: false);
    state = AsyncValue.data(current.copyWith(sessions: sessions, activeSessionId: sessionId));
  }

  void closeSession(String sessionId) {
    final current = state.valueOrNull ?? const TerminalState();
    ref.read(wsClientProvider).unsubscribe(sessionId);

    final sessions = Map<String, TerminalSessionState>.from(current.sessions)
      ..remove(sessionId);

    final nextActive = sessionId == current.activeSessionId
        ? (sessions.isEmpty ? null : sessions.keys.last)
        : current.activeSessionId;

    state = AsyncValue.data(TerminalState(
      sessions:        sessions,
      activeSessionId: nextActive,
    ));
  }

  void sendInput(String sessionId, Uint8List data) {
    ref.read(wsClientProvider).sendInput(sessionId, data);
  }

  void sendResize(String sessionId, int cols, int rows) {
    ref.read(wsClientProvider).sendResize(sessionId, cols, rows);
  }

  void _onPacket(Packet pkt) {
    switch (pkt.type) {
      case typeTerminalOutput:
      case typeScrollback:
        _deliverOutput(pkt.session, pkt.payload);

      case typeScrollbackDone:
        // Scrollback replay complete — nothing to do; terminal is already current.

      case typeSessionStatus:
        _updateSessionStatus(pkt.payload);

      case typePing:
        ref.read(wsClientProvider).send(
          Packet(type: typePong).encode(),
        );
    }
  }

  void _deliverOutput(String? sessionId, Uint8List? payload) {
    if (sessionId == null || payload == null) return;
    final current = state.valueOrNull;
    if (current == null) return;
    final sess = current.sessions[sessionId];
    if (sess == null) return;

    sess.terminal.write(String.fromCharCodes(payload));

    final isActive = sessionId == current.activeSessionId;
    if (!isActive) {
      final sessions = Map<String, TerminalSessionState>.from(current.sessions);
      sessions[sessionId] = sess.copyWith(hasNewOutput: true);
      state = AsyncValue.data(current.copyWith(sessions: sessions));
    }
  }

  void _updateSessionStatus(Uint8List? payload) {
    if (payload == null) return;
    final u = Unpacker.fromList(payload);
    final mapLen = u.unpackMapLength();
    String? id, status;
    int? exitCode;
    for (int i = 0; i < mapLen; i++) {
      final k = u.unpackString()!;
      switch (k) {
        case 'id':        id       = u.unpackString();
        case 'status':    status   = u.unpackString();
        case 'exit_code': exitCode = u.unpackInt();
        default:          u.unpackString();
      }
    }
    if (id == null || status == null) return;
    final current = state.valueOrNull;
    if (current == null) return;
    final sess = current.sessions[id];
    if (sess == null) return;
    final sessions = Map<String, TerminalSessionState>.from(current.sessions);
    sessions[id] = sess.copyWith(status: status, exitCode: exitCode);
    state = AsyncValue.data(current.copyWith(sessions: sessions));
  }
}

final terminalProvider =
    AsyncNotifierProvider<TerminalNotifier, TerminalState>(() => TerminalNotifier());
