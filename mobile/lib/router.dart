import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'features/auth/auth_provider.dart';
import 'features/auth/login_page.dart';
import 'features/auth/register_page.dart';
import 'features/workspace/session_list_page.dart';
import 'features/terminal/terminal_page.dart';

final routerProvider = Provider<GoRouter>((ref) {
  final authState = ref.watch(authProvider);

  return GoRouter(
    initialLocation: '/login',
    redirect: (context, state) {
      final isLoading = authState.isLoading;
      if (isLoading) return null;

      final isLoggedIn = authState.value is AuthLoggedIn;
      final path = state.uri.path;
      final goingToAuth = path == '/login' || path == '/register';

      if (!isLoggedIn && !goingToAuth) return '/login';
      if (isLoggedIn && goingToAuth) return '/sessions';
      return null;
    },
    routes: [
      GoRoute(path: '/login',    builder: (_, __) => const LoginPage()),
      GoRoute(path: '/register', builder: (_, __) => const RegisterPage()),
      GoRoute(path: '/sessions', builder: (_, __) => const SessionListPage()),
      GoRoute(path: '/terminal', builder: (_, __) => const TerminalPage()),
    ],
  );
});
