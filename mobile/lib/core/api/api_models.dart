/// API response/request models — hand-written JSON serialisation (no codegen).

class AuthResponse {
  final String accessToken;
  final String refreshToken;
  final String userID;

  const AuthResponse({
    required this.accessToken,
    required this.refreshToken,
    required this.userID,
  });

  factory AuthResponse.fromJson(Map<String, dynamic> json) => AuthResponse(
        accessToken:  json['access_token']  as String,
        refreshToken: json['refresh_token'] as String,
        userID:       json['user_id']       as String,
      );
}

class DeviceModel {
  final String id;
  final String name;
  final String platform;
  final bool online;

  const DeviceModel({
    required this.id,
    required this.name,
    required this.platform,
    required this.online,
  });

  factory DeviceModel.fromJson(Map<String, dynamic> json) => DeviceModel(
        id:       json['id']       as String,
        name:     json['name']     as String,
        platform: json['platform'] as String,
        online:   (json['online']  as bool?) ?? false,
      );

  bool get isOnline => online;
}

class SessionModel {
  final String id;
  final String name;
  final String command;
  final String status; // active | exited | killed
  final int? exitCode;
  final String startedAt;
  final String lastActivity;
  final bool tmuxBacked;
  final String tmuxTarget;

  const SessionModel({
    required this.id,
    required this.name,
    required this.command,
    required this.status,
    this.exitCode,
    required this.startedAt,
    required this.lastActivity,
    this.tmuxBacked = false,
    this.tmuxTarget = '',
  });

  factory SessionModel.fromJson(Map<String, dynamic> json) => SessionModel(
        id:           json['id']            as String,
        name:         (json['name'] as String?) ?? '',
        command:      json['command']       as String,
        status:       json['status']        as String,
        exitCode:     json['exit_code']     as int?,
        startedAt:    json['started_at']    as String,
        lastActivity: json['last_activity'] as String,
        tmuxBacked:   (json['tmux_backed']  as bool?) ?? false,
        tmuxTarget:   (json['tmux_target']  as String?) ?? '',
      );

  bool get isActive => status == 'active';
}

// ── Tmux tree models ──────────────────────────────────────────────────────────

class TmuxPaneNode {
  final String ccmuxId;
  final String title;
  final bool active;
  const TmuxPaneNode({required this.ccmuxId, required this.title, required this.active});
}

class TmuxWindowNode {
  final int index;
  final String name;
  final List<TmuxPaneNode> panes;
  const TmuxWindowNode({required this.index, required this.name, required this.panes});
}

class TmuxSessionNode {
  final String name;
  final List<TmuxWindowNode> windows;
  const TmuxSessionNode({required this.name, required this.windows});
}

class TmuxTree {
  final String deviceId;
  final List<TmuxSessionNode> sessions;
  const TmuxTree({required this.deviceId, required this.sessions});
}

// ── Device Metrics ────────────────────────────────────────────────────────────

/// Real-time CPU and memory snapshot streamed from the agent every ~5 s.
class DeviceMetrics {
  final double cpuPercent; // 0–100
  final int memUsedMB;
  final int memTotalMB;

  const DeviceMetrics({
    required this.cpuPercent,
    required this.memUsedMB,
    required this.memTotalMB,
  });

  double get memUsedRatio =>
      memTotalMB > 0 ? memUsedMB / memTotalMB : 0.0;
}
