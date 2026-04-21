import 'package:firebase_core/firebase_core.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/theme.dart';
import 'features/notifications/notification_handler.dart';
import 'features/notifications/push_service.dart';
import 'features/auth/auth_provider.dart';
import 'router.dart';

bool firebaseAvailable = false;

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  try {
    await Firebase.initializeApp();
    await initNotifications();
    firebaseAvailable = true;
  } catch (e) {
    // Firebase not configured yet — push notifications disabled.
    debugPrint('Firebase init skipped: $e');
  }
  runApp(const ProviderScope(child: CcmuxApp()));
}

class CcmuxApp extends ConsumerStatefulWidget {
  const CcmuxApp({super.key});

  @override
  ConsumerState<CcmuxApp> createState() => _CcmuxAppState();
}

class _CcmuxAppState extends ConsumerState<CcmuxApp> {
  @override
  void initState() {
    super.initState();
    // After auth succeeds, register the push token.
    if (firebaseAvailable) {
      ref.listenManual(authProvider, (prev, next) {
        if (next.value is AuthLoggedIn) {
          ref.read(pushServiceProvider).init();
        }
      });
    }
    // Handle notification tap from terminated state.
    if (firebaseAvailable) {
      getInitialSessionId().then((sid) {
        if (sid != null) {
          // TODO: open the session once the terminal provider is ready.
        }
      });
      listenNotificationTaps((sid) {
        // TODO: navigate to session by sid using the router.
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final router = ref.watch(routerProvider);
    return MaterialApp.router(
      title: 'ccmux',
      theme: buildCcmuxTheme(),
      routerConfig: router,
    );
  }
}
