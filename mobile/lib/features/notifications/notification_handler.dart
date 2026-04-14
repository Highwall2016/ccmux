import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

/// Must be a top-level function (not a method) — FCM requirement.
@pragma('vm:entry-point')
Future<void> firebaseMessagingBackgroundHandler(RemoteMessage message) async {
  // Background messages are handled by the OS notification tray.
  // We only need this registration point; no extra work required.
}

/// Initialise local notifications and register FCM listeners.
/// Call once in [main] after Firebase.initializeApp().
Future<void> initNotifications() async {
  FirebaseMessaging.onBackgroundMessage(firebaseMessagingBackgroundHandler);

  final localNotif = FlutterLocalNotificationsPlugin();
  const android = AndroidInitializationSettings('@mipmap/ic_launcher');
  const ios     = DarwinInitializationSettings();
  await localNotif.initialize(
    const InitializationSettings(android: android, iOS: ios),
  );

  // Show a local notification when a FCM message arrives while the app is
  // in the foreground (FCM does not show banners automatically in foreground).
  FirebaseMessaging.onMessage.listen((msg) {
    final n = msg.notification;
    if (n == null) return;
    localNotif.show(
      msg.hashCode,
      n.title,
      n.body,
      NotificationDetails(
        android: AndroidNotificationDetails(
          'ccmux_alerts',
          'ccmux alerts',
          importance: Importance.high,
          priority:   Priority.high,
        ),
        iOS: const DarwinNotificationDetails(),
      ),
    );
  });
}

/// Returns the session_id from a notification tap, if any.
/// Call at app startup to handle taps when the app was terminated.
Future<String?> getInitialSessionId() async {
  final msg = await FirebaseMessaging.instance.getInitialMessage();
  return msg?.data['session_id'] as String?;
}

/// Listen for notification taps while the app is in background (not terminated).
/// [onTap] receives the session_id string.
void listenNotificationTaps(void Function(String sessionId) onTap) {
  FirebaseMessaging.onMessageOpenedApp.listen((msg) {
    final sid = msg.data['session_id'] as String?;
    if (sid != null) onTap(sid);
  });
}
