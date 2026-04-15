import 'dart:io';
import 'package:device_info_plus/device_info_plus.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/api/api_client.dart';

/// Handles FCM token registration and permission requests.
class PushService {
  final ApiClient _api;

  PushService(this._api);

  /// Call once after successful login.
  Future<void> init() async {
    // Request permission (iOS shows a dialog; Android 13+ also needs it).
    final settings = await FirebaseMessaging.instance.requestPermission(
      alert:         true,
      badge:         true,
      sound:         true,
      announcement:  false,
      carPlay:       false,
      criticalAlert: false,
      provisional:   false,
    );

    if (settings.authorizationStatus == AuthorizationStatus.denied) return;

    final token = await FirebaseMessaging.instance.getToken();
    if (token == null) return;

    await _registerToken(token);

    // Re-register if the token rotates.
    FirebaseMessaging.instance.onTokenRefresh.listen(_registerToken);
  }

  Future<void> _registerToken(String token) async {
    final platform   = Platform.isIOS ? 'ios' : 'android';
    final deviceName = await _deviceName();
    try {
      await _api.registerPushToken(
        token:      token,
        platform:   platform,
        deviceName: deviceName,
      );
    } catch (_) {
      // Non-fatal — the app works without push.
    }
  }

  Future<String> _deviceName() async {
    final info = DeviceInfoPlugin();
    try {
      if (Platform.isIOS) {
        final d = await info.iosInfo;
        return '${d.name} (${d.model})';
      } else {
        final d = await info.androidInfo;
        return '${d.brand} ${d.model}';
      }
    } catch (_) {
      return 'unknown device';
    }
  }
}

final pushServiceProvider = Provider<PushService>((ref) {
  return PushService(ref.watch(apiClientProvider));
});
