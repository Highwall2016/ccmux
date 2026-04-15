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

  Future<void> renameSession(String deviceId, String sessionId, String name) async {
    final api = ref.read(apiClientProvider);
    await api.renameSession(deviceId, sessionId, name);
    // Optimistic update: reflect the new name immediately.
    // The WS TypeSessionStatus broadcast only reaches subscribed sessions;
    // the workspace drawer is not subscribed, so we update local state here.
    _patchSession(deviceId, sessionId, name: name);
  }

  Future<void> killSession(String deviceId, String sessionId) async {
    final api = ref.read(apiClientProvider);
    await api.killSession(deviceId, sessionId);
    // Optimistic update: mark as killed immediately so the drawer removes it
    // from the active list.  The WS broadcast only reaches subscribed clients.
    _patchSession(deviceId, sessionId, status: 'killed');
  }

  /// Patches a single session's fields in local state without a full refresh.
  void _patchSession(
    String deviceId,
    String sessionId, {
    String? status,
    String? name,
  }) {
    final current = state.valueOrNull;
    if (current == null) return;

    final sessionsForDevice = current.sessionsByDevice[deviceId];
    if (sessionsForDevice == null) return;

    final idx = sessionsForDevice.indexWhere((s) => s.id == sessionId);
    if (idx == -1) return;

    final old = sessionsForDevice[idx];
    final updated = SessionModel(
      id:           old.id,
      name:         name   ?? old.name,
      command:      old.command,
      status:       status ?? old.status,
      exitCode:     old.exitCode,
      startedAt:    old.startedAt,
      lastActivity: old.lastActivity,
    );

    final newList = List<SessionModel>.from(sessionsForDevice);
    newList[idx] = updated;

    state = AsyncValue.data(current.copyWith(
      sessionsByDevice: {
        ...current.sessionsByDevice,
        deviceId: newList,
      },
    ));
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
    String? id, status, name;
    try {
      final u = Unpacker.fromList(pkt.payload!);
      final mapLen = u.unpackMapLength();
      for (int i = 0; i < mapLen; i++) {
        final k = u.unpackString()!;
        switch (k) {
          case 'id':        id     = u.unpackString();
          case 'status':    status = u.unpackString();
          case 'name':      name   = u.unpackString();
          // Consume known non-string fields so the unpacker stays in sync.
          case 'exit_code': u.unpackInt();
          default:          u.unpackString(); // cmd is a string
        }
      }
    } catch (_) {
      // Malformed packet — fall back to a full refresh below.
    }
    if (id == null || status == null) return;

    final current = state.valueOrNull;
    if (current == null) return;

    // Try to find the session in the existing map.
    final updated = Map<String, List<SessionModel>>.from(current.sessionsByDevice);
    bool found = false;
    for (final entry in updated.entries) {
      final idx = entry.value.indexWhere((s) => s.id == id);
      if (idx != -1) {
        found = true;
        final old = entry.value[idx];
        final newList = List<SessionModel>.from(entry.value);
        newList[idx] = SessionModel(
          id:           old.id,
          name:         name ?? old.name,
          command:      old.command,
          status:       status,
          exitCode:     old.exitCode,
          startedAt:    old.startedAt,
          lastActivity: old.lastActivity,
        );
        updated[entry.key] = newList;
        break;
      }
    }

    if (found) {
      state = AsyncValue.data(current.copyWith(sessionsByDevice: updated));
    } else if (status == 'active') {
      // New session announced by the agent — fetch the full list from the API.
      refresh();
    }
  }
}

final workspaceProvider =
    AsyncNotifierProvider<WorkspaceNotifier, WorkspaceState>(() => WorkspaceNotifier());
