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

  /// Latest tmux topology keyed by device ID.
  final Map<String, TmuxTree> tmuxTreeByDevice;

  /// Latest real-time CPU/memory metrics keyed by device ID.
  /// Null for a device until the first TypeDeviceMetrics packet arrives.
  final Map<String, DeviceMetrics> metricsByDevice;

  const WorkspaceState({
    this.devices = const [],
    this.sessionsByDevice = const {},
    this.loading = false,
    this.error,
    this.tmuxTreeByDevice = const {},
    this.metricsByDevice = const {},
  });

  WorkspaceState copyWith({
    List<DeviceModel>? devices,
    Map<String, List<SessionModel>>? sessionsByDevice,
    bool? loading,
    String? error,
    Map<String, TmuxTree>? tmuxTreeByDevice,
    Map<String, DeviceMetrics>? metricsByDevice,
  }) =>
      WorkspaceState(
        devices: devices ?? this.devices,
        sessionsByDevice: sessionsByDevice ?? this.sessionsByDevice,
        loading: loading ?? this.loading,
        error: error,
        tmuxTreeByDevice: tmuxTreeByDevice ?? this.tmuxTreeByDevice,
        metricsByDevice: metricsByDevice ?? this.metricsByDevice,
      );
}

class WorkspaceNotifier extends AsyncNotifier<WorkspaceState> {
  @override
  Future<WorkspaceState> build() async {
    // Listen for real-time packets from the WS.
    final ws = ref.watch(wsClientProvider);
    ws.packets
        .where((p) => p.type == typeSessionStatus)
        .listen(_onSessionStatus);
    ws.packets.where((p) => p.type == typeTmuxTree).listen(_onTmuxTree);
    ws.packets
        .where((p) => p.type == typeDeviceMetrics)
        .listen(_onDeviceMetrics);
    return _fetchAll();
  }

  Future<WorkspaceState> _fetchAll() async {
    final api = ref.read(apiClientProvider);
    final devices = await api.listDevices();
    final Map<String, List<SessionModel>> sessMap = {};
    await Future.wait(devices.map((d) async {
      sessMap[d.id] = await api.listSessions(d.id);
    }));

    // Force device subscriptions for old backend versions.
    // The old backend only adds us to deviceSubs if we subscribe to a session.
    // By subscribing and immediately unsubscribing, we remain in deviceSubs
    // (since it's only cleared on disconnect) and receive TypeTmuxTree / TypeDeviceMetrics.
    Future<void> forceSubscribe() async {
      final ws = ref.read(wsClientProvider);
      for (int i = 0; i < 5; i++) {
        if (ws.state == WsState.connected) {
          for (final entry in sessMap.entries) {
            final activeSessions = entry.value.where((s) => s.status == 'active').toList();
            if (activeSessions.isNotEmpty) {
              final sid = activeSessions.first.id;
              ws.subscribe(sid);
              ws.unsubscribe(sid);
            }
          }
          break;
        }
        await Future.delayed(const Duration(seconds: 1));
      }
    }
    forceSubscribe();

    return WorkspaceState(devices: devices, sessionsByDevice: sessMap);
  }

  Future<void> renameSession(
      String deviceId, String sessionId, String name) async {
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

  /// Spawns a new session on [deviceId]. Returns the new session ID.
  Future<String> spawnSession(
    String deviceId, {
    String name = '',
    String command = 'bash',
    bool useTmux = false,
    bool tmuxSplit = false,
  }) async {
    final api = ref.read(apiClientProvider);
    final sessionId = await api.spawnSession(deviceId,
        name: name, command: command, useTmux: useTmux, tmuxSplit: tmuxSplit);
    // Optimistically insert the new session so the WS "active" event finds it
    // via _patchSession instead of falling through to refresh(), which would
    // rebuild the drawer while it is animating out and cause a use-after-dispose crash.
    final now = DateTime.now().toIso8601String();
    _insertSession(
      deviceId,
      SessionModel(
        id: sessionId,
        name: name,
        command: command.isEmpty ? 'bash' : command,
        status: 'active',
        startedAt: now,
        lastActivity: now,
        tmuxBacked: useTmux,
      ),
    );
    return sessionId;
  }

  void _insertSession(String deviceId, SessionModel session) {
    final current = state.valueOrNull;
    if (current == null) return;
    final existing = current.sessionsByDevice[deviceId] ?? [];
    if (existing.any((s) => s.id == session.id)) return;
    state = AsyncValue.data(current.copyWith(
      sessionsByDevice: {
        ...current.sessionsByDevice,
        deviceId: [...existing, session],
      },
    ));
  }

  /// Patches a single session's fields in local state without a full refresh.
  void _patchSession(
    String deviceId,
    String sessionId, {
    String? status,
    String? name,
    bool? tmuxBacked,
    String? tmuxTarget,
  }) {
    final current = state.valueOrNull;
    if (current == null) return;

    final sessionsForDevice = current.sessionsByDevice[deviceId];
    if (sessionsForDevice == null) return;

    final idx = sessionsForDevice.indexWhere((s) => s.id == sessionId);
    if (idx == -1) return;

    final old = sessionsForDevice[idx];
    final updated = SessionModel(
      id: old.id,
      name: name ?? old.name,
      command: old.command,
      status: status ?? old.status,
      exitCode: old.exitCode,
      startedAt: old.startedAt,
      lastActivity: old.lastActivity,
      tmuxBacked: tmuxBacked ?? old.tmuxBacked,
      tmuxTarget: tmuxTarget ?? old.tmuxTarget,
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

  /// Removes an already-ended session from local state (no API call needed).
  void removeEndedSession(String deviceId, String sessionId) {
    final current = state.valueOrNull;
    if (current == null) return;
    final sessions = current.sessionsByDevice[deviceId];
    if (sessions == null) return;
    state = AsyncValue.data(current.copyWith(
      sessionsByDevice: {
        ...current.sessionsByDevice,
        deviceId: sessions.where((s) => s.id != sessionId).toList(),
      },
    ));
  }

  Future<void> removeDevice(String deviceId) async {
    final api = ref.read(apiClientProvider);
    await api.deleteDevice(deviceId);
    final current = state.valueOrNull;
    if (current == null) return;
    final newDevices = current.devices.where((d) => d.id != deviceId).toList();
    final newSessions =
        Map<String, List<SessionModel>>.from(current.sessionsByDevice)
          ..remove(deviceId);
    state = AsyncValue.data(current.copyWith(
      devices: newDevices,
      sessionsByDevice: newSessions,
    ));
  }

  Future<void> refresh() async {
    // Do NOT set state to loading — that would set state.valueOrNull to null
    // and cause _onTmuxTree / _onDeviceMetrics to drop in-flight heartbeats,
    // which results in the metrics display permanently showing '--'.
    // Instead, fetch silently and merge the fresh device/session data while
    // preserving any live metrics and tmux topology already in state.
    try {
      final fresh = await _fetchAll();
      final current = state.valueOrNull;
      state = AsyncValue.data(fresh.copyWith(
        metricsByDevice: current?.metricsByDevice ?? const {},
        tmuxTreeByDevice: current?.tmuxTreeByDevice ?? const {},
      ));
    } catch (_) {
      // Silently swallow network errors during background refresh so an
      // intermittent failure does not destroy the existing UI state.
    }
  }

  void _onSessionStatus(Packet pkt) {
    if (pkt.payload == null) return;
    String? id, status, name, tmuxTarget;
    bool tmuxBacked = false;
    try {
      final u = Unpacker.fromList(pkt.payload!);
      final mapLen = u.unpackMapLength();
      for (int i = 0; i < mapLen; i++) {
        final k = u.unpackString()!;
        switch (k) {
          case 'id':
            id = u.unpackString();
          case 'status':
            status = u.unpackString();
          case 'name':
            name = u.unpackString();
          case 'tmux_target':
            tmuxTarget = u.unpackString();
          case 'tmux_backed':
            tmuxBacked = u.unpackBool() ?? false;
          // Consume known non-string fields so the unpacker stays in sync.
          case 'exit_code':
            u.unpackInt();
          default:
            u.unpackString(); // cmd is a string
        }
      }
    } catch (_) {
      // Malformed packet — fall back to a full refresh below.
    }
    if (id == null || status == null) return;

    final current = state.valueOrNull;
    if (current == null) return;

    // Try to find the session in the existing map.
    final updated =
        Map<String, List<SessionModel>>.from(current.sessionsByDevice);
    bool found = false;
    for (final entry in updated.entries) {
      final idx = entry.value.indexWhere((s) => s.id == id);
      if (idx != -1) {
        found = true;
        final old = entry.value[idx];
        final newList = List<SessionModel>.from(entry.value);
        newList[idx] = SessionModel(
          id: old.id,
          name: name ?? old.name,
          command: old.command,
          status: status,
          exitCode: old.exitCode,
          startedAt: old.startedAt,
          lastActivity: old.lastActivity,
          tmuxBacked: tmuxBacked || old.tmuxBacked,
          tmuxTarget: (tmuxTarget != null && tmuxTarget.isNotEmpty)
              ? tmuxTarget
              : old.tmuxTarget,
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

  /// Handles a TypeTmuxTree packet, updating the tmux topology for its device.
  /// Also reads optional cpu/mem_used/mem_total fields added by the metrics
  /// collector goroutine — a backwards-compatible piggyback that works with
  /// older backend deployments which already forward TypeTmuxTree unchanged.
  void _onTmuxTree(Packet pkt) {
    final payload = pkt.payload;
    if (payload == null) return;
    try {
      final u = Unpacker.fromList(payload);
      final mapLen = u.unpackMapLength();
      String deviceId = '';
      final sessionNodes = <TmuxSessionNode>[];
      double? cpu;
      int? memUsed;
      int? memTotal;

      for (int i = 0; i < mapLen; i++) {
        final k = u.unpackString()!;
        if (k == 'device_id') {
          deviceId = u.unpackString() ?? '';
        } else if (k == 'sessions') {
          final sessCount = u.unpackListLength();
          for (int s = 0; s < sessCount; s++) {
            final sm = u.unpackMapLength();
            String sessName = '';
            final windows = <TmuxWindowNode>[];
            for (int sf = 0; sf < sm; sf++) {
              final sk = u.unpackString()!;
              if (sk == 'name') {
                sessName = u.unpackString() ?? '';
              } else if (sk == 'windows') {
                final winCount = u.unpackListLength();
                for (int w = 0; w < winCount; w++) {
                  final wm = u.unpackMapLength();
                  int winIndex = 0;
                  String winName = '';
                  final panes = <TmuxPaneNode>[];
                  for (int wf = 0; wf < wm; wf++) {
                    final wk = u.unpackString()!;
                    if (wk == 'index') {
                      winIndex = u.unpackInt() ?? 0;
                    } else if (wk == 'name') {
                      winName = u.unpackString() ?? '';
                    } else if (wk == 'panes') {
                      final paneCount = u.unpackListLength();
                      for (int p = 0; p < paneCount; p++) {
                        final pm = u.unpackMapLength();
                        int _paneIndex = 0;
                        String ccmuxId = '';
                        String title = '';
                        bool active = false;
                        for (int pf = 0; pf < pm; pf++) {
                          final pk = u.unpackString()!;
                          if (pk == 'index') {
                            _paneIndex = u.unpackInt() ?? 0;
                          } else if (pk == 'id') {
                            ccmuxId = u.unpackString() ?? '';
                          } else if (pk == 'title') {
                            title = u.unpackString() ?? '';
                          } else if (pk == 'active') {
                            active = u.unpackBool() ?? false;
                          } else {
                            u.unpackString();
                          }
                        }
                        if (ccmuxId.isNotEmpty) {
                          panes.add(TmuxPaneNode(
                            ccmuxId: ccmuxId,
                            title: title,
                            active: active,
                          ));
                        }
                      }
                    } else {
                      u.unpackString();
                    }
                  }
                  windows.add(TmuxWindowNode(
                      index: winIndex, name: winName, panes: panes));
                }
              } else {
                u.unpackString();
              }
            }
            sessionNodes.add(TmuxSessionNode(name: sessName, windows: windows));
          }
        } else if (k == 'cpu') {
          cpu = u.unpackDouble()?.toDouble();
        } else if (k == 'mem_used') {
          memUsed = u.unpackInt();
        } else if (k == 'mem_total') {
          memTotal = u.unpackInt();
        } else {
          u.unpackString();
        }
      }

      if (deviceId.isEmpty) return;
      final current = state.valueOrNull;
      if (current == null) return;

      // Update tmux topology only when there are sessions in this packet
      // (metrics-only heartbeats have an empty sessions list).
      final newTmux = sessionNodes.isNotEmpty
          ? {
              ...current.tmuxTreeByDevice,
              deviceId: TmuxTree(deviceId: deviceId, sessions: sessionNodes),
            }
          : current.tmuxTreeByDevice;

      // Update metrics whenever the fields are present.
      final newMetrics = (cpu != null && memUsed != null && memTotal != null)
          ? {
              ...current.metricsByDevice,
              deviceId: DeviceMetrics(
                cpuPercent: cpu,
                memUsedMB: memUsed,
                memTotalMB: memTotal,
              ),
            }
          : current.metricsByDevice;

      state = AsyncValue.data(current.copyWith(
        tmuxTreeByDevice: newTmux,
        metricsByDevice: newMetrics,
      ));
    } catch (_) {
      // Malformed TmuxTree packet — ignore.
    }
  }
  /// Handles a TypeDeviceMetrics packet, updating live CPU/memory for its device.
  void _onDeviceMetrics(Packet pkt) {
    final payload = pkt.payload;
    if (payload == null) return;
    try {
      final u = Unpacker.fromList(payload);
      final mapLen = u.unpackMapLength();
      String deviceId = '';
      double cpu = 0;
      int memUsed = 0;
      int memTotal = 0;

      for (int i = 0; i < mapLen; i++) {
        final k = u.unpackString()!;
        switch (k) {
          case 'device_id':
            deviceId = u.unpackString() ?? '';
          case 'cpu':
            cpu = (u.unpackDouble() ?? 0).toDouble();
          case 'mem_used':
            memUsed = u.unpackInt() ?? 0;
          case 'mem_total':
            memTotal = u.unpackInt() ?? 0;
          default:
            u.unpackString();
        }
      }

      if (deviceId.isEmpty) return;
      final current = state.valueOrNull;
      if (current == null) return;

      state = AsyncValue.data(current.copyWith(
        metricsByDevice: {
          ...current.metricsByDevice,
          deviceId: DeviceMetrics(
            cpuPercent: cpu,
            memUsedMB: memUsed,
            memTotalMB: memTotal,
          ),
        },
      ));
    } catch (_) {
      // Malformed metrics packet — ignore.
    }
  }
}

final workspaceProvider =
    AsyncNotifierProvider<WorkspaceNotifier, WorkspaceState>(
        () => WorkspaceNotifier());

/// Tracks which device is shown in the session list. Null = first device.
final selectedDeviceIdProvider = StateProvider<String?>((ref) => null);
