import 'package:flutter_secure_storage/flutter_secure_storage.dart';

const _storage = FlutterSecureStorage(
  aOptions: AndroidOptions(encryptedSharedPreferences: true),
);

const _keyAccess  = 'access_token';
const _keyRefresh = 'refresh_token';
const _keyUserID  = 'user_id';

Future<void> saveTokens({
  required String accessToken,
  required String refreshToken,
  required String userID,
}) async {
  await Future.wait([
    _storage.write(key: _keyAccess,  value: accessToken),
    _storage.write(key: _keyRefresh, value: refreshToken),
    _storage.write(key: _keyUserID,  value: userID),
  ]);
}

Future<({String? accessToken, String? refreshToken, String? userID})>
    loadTokens() async {
  final results = await Future.wait([
    _storage.read(key: _keyAccess),
    _storage.read(key: _keyRefresh),
    _storage.read(key: _keyUserID),
  ]);
  return (
    accessToken:  results[0],
    refreshToken: results[1],
    userID:       results[2],
  );
}

Future<void> clearTokens() async {
  await Future.wait([
    _storage.delete(key: _keyAccess),
    _storage.delete(key: _keyRefresh),
    _storage.delete(key: _keyUserID),
  ]);
}

/// Returns true if the access token JWT is not yet expired (with a 60 s buffer).
bool isAccessTokenValid(String? token) {
  if (token == null || token.isEmpty) return false;
  try {
    final parts = token.split('.');
    if (parts.length != 3) return false;
    // Base64url-decode the payload (part index 1).
    String payload = parts[1];
    // Pad to multiple of 4.
    while (payload.length % 4 != 0) payload += '=';
    final decoded = Uri.decodeFull(payload
        .replaceAll('-', '+')
        .replaceAll('_', '/'));
    // Simple scan for "exp":<number> without importing dart:convert.
    final expMatch = RegExp(r'"exp"\s*:\s*(\d+)').firstMatch(decoded);
    if (expMatch == null) return false;
    final exp = int.parse(expMatch.group(1)!);
    final now = DateTime.now().millisecondsSinceEpoch ~/ 1000;
    return exp > now + 60; // 60-second buffer
  } catch (_) {
    return false;
  }
}
