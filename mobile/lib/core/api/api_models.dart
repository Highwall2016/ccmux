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
  final String? lastSeen;

  const DeviceModel({
    required this.id,
    required this.name,
    required this.platform,
    this.lastSeen,
  });

  factory DeviceModel.fromJson(Map<String, dynamic> json) => DeviceModel(
        id:       json['id']       as String,
        name:     json['name']     as String,
        platform: json['platform'] as String,
        lastSeen: json['last_seen'] as String?,
      );

  /// Returns true if the device connected within the last 90 seconds.
  bool get isOnline {
    if (lastSeen == null) return false;
    final t = DateTime.tryParse(lastSeen!);
    if (t == null) return false;
    return DateTime.now().difference(t).inSeconds < 90;
  }
}

class SessionModel {
  final String id;
  final String name;
  final String command;
  final String status; // active | exited | killed
  final int? exitCode;
  final String startedAt;
  final String lastActivity;

  const SessionModel({
    required this.id,
    required this.name,
    required this.command,
    required this.status,
    this.exitCode,
    required this.startedAt,
    required this.lastActivity,
  });

  factory SessionModel.fromJson(Map<String, dynamic> json) => SessionModel(
        id:           json['id']            as String,
        name:         (json['name'] as String?) ?? '',
        command:      json['command']       as String,
        status:       json['status']        as String,
        exitCode:     json['exit_code']     as int?,
        startedAt:    json['started_at']    as String,
        lastActivity: json['last_activity'] as String,
      );

  bool get isActive => status == 'active';
}
