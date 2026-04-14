import 'dart:typed_data';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:messagepack/messagepack.dart';
import '../../core/api/api_client.dart';
import '../../core/api/api_models.dart';
import '../../core/websocket/ws_client.dart';
import '../../core/protocol/packet.dart';

/// Combined device + sessions state.
class WorkspaceState {
  final List<DeviceModel> devices;
  final Map<String, List<SessionModel>> sessionsByDevice;
  final bool loading;
  final String? error;

  const WorkspaceState({
    this.devices = const [],
    this.sessionsByDevice = const {},
    this.loading = false,
    this.error,
  });

  WorkspaceState copyWith({
    List<DeviceModel>? devices,
    Map<String, List<SessionModel>>? sessionsByDevice,
    bool? loading,
    String? error,
  }) =>
      WorkspaceState(
        devices:           devices           ?? this.devices,
        sessionsByDevice:  sessionsByDevice  ?? this.sessionsByDevice,
        loading:           loading           ?? this.loading,
        error:             error,
      );
}

class WorkspaceNotifier extends AsyncNotifier<WorkspaceState> {
  @override
  Future<WorkspaceState> build() async {
    // Listen for real-time session status changes from the WS.
    final ws = ref.watch(wsClientProvider);
    ws.packets.where((p) => p.type == typeSessionStatus).listen(_onSessionStatus);
    return _fetchAll();
  }

  Future<WorkspaceState> _fetchAll() async {
    final api = ref.read(apiClientProvider);
    final devices = await api.listDevices();
    final Map<String, List<SessionModel>> sessMap = {};
    await Future.wait(devices.map((d) async {
      sessMap[d.id] = await api.listSessions(d.id);
    }));
    return WorkspaceState(devices: devices, sessionsByDevice: sessMap);
  }

  Future<void> refresh() async {
    state = const AsyncValue.loading();
    try {
      state = AsyncValue.data(await _fetchAll());
    } catch (e) {
      state = AsyncValue.error(e, StackTrace.current);
    }
  }

  void _onSessionStatus(Packet pkt) {
    if (pkt.payload == null) return;
    final u = Unpacker.fromList(pkt.payload!);
    final mapLen = u.unpackMapLength();
    String? id, status;
    for (int i = 0; i < mapLen; i++) {
      final k = u.unpackString()!;
      switch (k) {
        case 'id':     id     = u.unpackString();
        case 'status': status = u.unpackString();
        default:       u.unpackString();
      }
    }
    if (id == null || status == null) return;

    final current = state.valueOrNull;
    if (current == null) return;

    final updated = Map<String, List<SessionModel>>.from(current.sessionsByDevice);
    for (final entry in updated.entries) {
      final idx = entry.value.indexWhere((s) => s.id == id);
      if (idx != -1) {
        final old = entry.value[idx];
        final newSess = SessionModel(
          id:           old.id,
          name:         old.name,
          command:      old.command,
          status:       status,
          exitCode:     old.exitCode,
          startedAt:    old.startedAt,
          lastActivity: old.lastActivity,
        );
        final newList = List<SessionModel>.from(entry.value);
        newList[idx] = newSess;
        updated[entry.key] = newList;
        break;
      }
    }
    state = AsyncValue.data(current.copyWith(sessionsByDevice: updated));
  }
}

final workspaceProvider =
    AsyncNotifierProvider<WorkspaceNotifier, WorkspaceState>(() => WorkspaceNotifier());
