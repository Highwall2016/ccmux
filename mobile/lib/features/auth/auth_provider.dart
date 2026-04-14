import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../core/api/api_client.dart';
import '../../core/api/api_models.dart';
import '../../core/storage/secure_storage.dart' as storage;

sealed class AuthState {
  const AuthState();
}

class AuthLoading  extends AuthState { const AuthLoading(); }
class AuthLoggedOut extends AuthState { const AuthLoggedOut(); }
class AuthLoggedIn  extends AuthState {
  final String userID;
  final String accessToken;
  const AuthLoggedIn({required this.userID, required this.accessToken});
}
class AuthError extends AuthState {
  final String message;
  const AuthError(this.message);
}

class AuthNotifier extends AsyncNotifier<AuthState> {
  @override
  Future<AuthState> build() async {
    final tokens = await storage.loadTokens();
    if (tokens.userID == null) return const AuthLoggedOut();

    // Try to get a valid access token.
    String? access = tokens.accessToken;
    if (!storage.isAccessTokenValid(access) && tokens.refreshToken != null) {
      try {
        final api = ref.read(apiClientProvider);
        access = await api.refresh(tokens.refreshToken!);
        await storage.saveTokens(
          accessToken:  access,
          refreshToken: tokens.refreshToken!,
          userID:       tokens.userID!,
        );
      } catch (_) {
        await storage.clearTokens();
        return const AuthLoggedOut();
      }
    }
    if (access == null) return const AuthLoggedOut();
    ref.read(apiClientProvider).setAccessToken(access);
    return AuthLoggedIn(userID: tokens.userID!, accessToken: access);
  }

  Future<void> login(String email, String password) async {
    state = const AsyncValue.loading();
    try {
      final resp = await ref.read(apiClientProvider).login(email, password);
      await _persist(resp);
    } catch (e) {
      state = AsyncValue.data(AuthError(e.toString()));
    }
  }

  Future<void> register(String email, String password) async {
    state = const AsyncValue.loading();
    try {
      final resp = await ref.read(apiClientProvider).register(email, password);
      await _persist(resp);
    } catch (e) {
      state = AsyncValue.data(AuthError(e.toString()));
    }
  }

  Future<void> logout() async {
    final tokens = await storage.loadTokens();
    if (tokens.refreshToken != null) {
      try {
        await ref.read(apiClientProvider).logout(tokens.refreshToken!);
      } catch (_) {}
    }
    ref.read(apiClientProvider).clearAuth();
    await storage.clearTokens();
    state = const AsyncValue.data(AuthLoggedOut());
  }

  Future<void> _persist(AuthResponse resp) async {
    await storage.saveTokens(
      accessToken:  resp.accessToken,
      refreshToken: resp.refreshToken,
      userID:       resp.userID,
    );
    ref.read(apiClientProvider).setAccessToken(resp.accessToken);
    state = AsyncValue.data(
      AuthLoggedIn(userID: resp.userID, accessToken: resp.accessToken),
    );
  }
}

final authProvider = AsyncNotifierProvider<AuthNotifier, AuthState>(() => AuthNotifier());
