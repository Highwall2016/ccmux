import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:messagepack/messagepack.dart';
import 'package:xterm/xterm.dart';

import '../../core/protocol/packet.dart';
import '../../core/websocket/ws_client.dart';
import '../../core/websocket/ws_reconnect.dart';
import 'modifier_provider.dart';

/// Per-session state.
class TerminalSessionState {
  final String id;
  // Nullable backing field so hot-reload artifacts never crash the getter.
  final String? _name;
  String get name {
    final n = _name;
    return (n != null && n.isNotEmpty) ? n : id.substring(0, 8);
  }

  final Terminal terminal;
  final String status; // active | exited | killed
  final int? exitCode;
  final bool hasNewOutput;
  /// True when the session is backed by a running tmux pane.
  /// Drives tmux-specific UI: Ctrl+B toolbar button, swipe gestures, window tabs.
  final bool tmuxBacked;

  TerminalSessionState({
    required this.id,
    String? name,
    required this.terminal,
    this.status = 'active',
    this.exitCode,
    this.hasNewOutput = false,
    this.tmuxBacked = false,
  }) : _name = (name != null && name.isNotEmpty) ? name : null;

  TerminalSessionState copyWith({
    String? name,
    String? status,
    int? exitCode,
    bool? hasNewOutput,
    bool? tmuxBacked,
  }) =>
      TerminalSessionState(
        id:           id,
        name:         (name != null && name.isNotEmpty) ? name : _name,
        terminal:     terminal,
        status:       status       ?? this.status,
        exitCode:     exitCode     ?? this.exitCode,
        hasNewOutput: hasNewOutput ?? this.hasNewOutput,
        tmuxBacked:   tmuxBacked   ?? this.tmuxBacked,
      );
}

class TerminalState {
  final Map<String, TerminalSessionState> sessions;
  final String? activeSessionId;

  const TerminalState({this.sessions = const {}, this.activeSessionId});

  /// Whether the currently active session is tmux-backed.
  bool get activeTmuxBacked => activeSessionId != null
      ? (sessions[activeSessionId]?.tmuxBacked ?? false)
      : false;

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
  void openSession(String sessionId, {String name = '', bool tmuxBacked = false}) {
    final current = state.valueOrNull ?? const TerminalState();
    if (current.sessions.containsKey(sessionId)) {
      state = AsyncValue.data(current.copyWith(activeSessionId: sessionId));
      return;
    }

    final terminal = Terminal(
      maxLines: 10000,
      onOutput: (data) {
        final modifier = ref.read(modifierProvider);
        if (modifier != ArmedModifier.none) {
          ref.read(modifierProvider.notifier).state = ArmedModifier.none;
          final bytes = utf8.encode(data);
          final out = <int>[];
          for (final b in bytes) {
            if (modifier == ArmedModifier.ctrl) {
              out.add(b & 0x1F);
            } else {
              out.addAll([0x1B, b]);
            }
          }
          sendInput(sessionId, Uint8List.fromList(out));
          return;
        }
        sendInput(sessionId, Uint8List.fromList(utf8.encode(data)));
      },
      onResize: (cols, rows, pixelWidth, pixelHeight) {
        sendResize(sessionId, cols, rows);
      },
    );
    final sessions = Map<String, TerminalSessionState>.from(current.sessions);
    sessions[sessionId] = TerminalSessionState(
      id: sessionId,
      name: name.isNotEmpty ? name : sessionId.substring(0, 8),
      terminal: terminal,
      tmuxBacked: tmuxBacked,
    );

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

    try {
      sess.terminal.write(utf8.decode(payload, allowMalformed: true));
    } catch (_) {
      return;
    }

    final isActive = sessionId == current.activeSessionId;
    if (!isActive) {
      final sessions = Map<String, TerminalSessionState>.from(current.sessions);
      sessions[sessionId] = sess.copyWith(hasNewOutput: true);
      state = AsyncValue.data(current.copyWith(sessions: sessions));
    }
  }

  void _updateSessionStatus(Uint8List? payload) {
    if (payload == null) return;
    String? id, status, name;
    int? exitCode;
    try {
      final u = Unpacker.fromList(payload);
      final mapLen = u.unpackMapLength();
      for (int i = 0; i < mapLen; i++) {
        final k = u.unpackString()!;
        switch (k) {
          case 'id':        id       = u.unpackString();
          case 'status':    status   = u.unpackString();
          case 'name':      name     = u.unpackString();
          case 'exit_code': exitCode = u.unpackInt();
          default:          u.unpackString(); // skip unknown string fields (e.g. cmd)
        }
      }
    } catch (_) {
      return;
    }
    if (id == null || status == null) return;
    final current = state.valueOrNull;
    if (current == null) return;
    final sess = current.sessions[id];
    if (sess == null) return;
    final sessions = Map<String, TerminalSessionState>.from(current.sessions);
    sessions[id] = sess.copyWith(
      status: status,
      exitCode: exitCode,
      name: (name != null && name.isNotEmpty) ? name : null,
    );
    state = AsyncValue.data(current.copyWith(sessions: sessions));
  }
}

final terminalProvider =
    AsyncNotifierProvider<TerminalNotifier, TerminalState>(() => TerminalNotifier());
