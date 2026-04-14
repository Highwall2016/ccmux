import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../storage/secure_storage.dart' as storage;
import 'api_models.dart';

final apiClientProvider = Provider<ApiClient>((ref) {
  return ApiClient(baseUrl: const String.fromEnvironment(
    'CCMUX_API_URL',
    defaultValue: 'http://localhost:8080',
  ));
});

class ApiClient {
  final Dio _dio;

  ApiClient({required String baseUrl})
      : _dio = Dio(BaseOptions(
          baseUrl: baseUrl,
          connectTimeout: const Duration(seconds: 10),
          receiveTimeout: const Duration(seconds: 10),
          headers: {'Content-Type': 'application/json'},
        )) {
    _dio.interceptors.add(_JwtInterceptor(_dio, baseUrl));
  }

  // Auth

  Future<AuthResponse> register(String email, String password) async {
    final r = await _dio.post<Map<String, dynamic>>('/api/auth/register',
        data: {'email': email, 'password': password});
    return AuthResponse.fromJson(r.data!);
  }

  Future<AuthResponse> login(String email, String password) async {
    final r = await _dio.post<Map<String, dynamic>>('/api/auth/login',
        data: {'email': email, 'password': password});
    return AuthResponse.fromJson(r.data!);
  }

  Future<String> refresh(String refreshToken) async {
    final r = await _dio.post<Map<String, dynamic>>('/api/auth/refresh',
        data: {'refresh_token': refreshToken});
    return r.data!['access_token'] as String;
  }

  Future<void> logout(String refreshToken) async {
    await _dio.post('/api/auth/logout', data: {'refresh_token': refreshToken});
  }

  // Devices

  Future<List<DeviceModel>> listDevices() async {
    final r = await _dio.get<List<dynamic>>('/api/devices');
    return r.data!.map((e) => DeviceModel.fromJson(e as Map<String, dynamic>)).toList();
  }

  // Sessions

  Future<List<SessionModel>> listSessions(String deviceId) async {
    final r = await _dio.get<List<dynamic>>('/api/devices/$deviceId/sessions');
    return r.data!.map((e) => SessionModel.fromJson(e as Map<String, dynamic>)).toList();
  }

  // Push tokens

  Future<void> registerPushToken({
    required String token,
    required String platform,
    required String deviceName,
  }) async {
    await _dio.post('/api/push/register',
        data: {'token': token, 'platform': platform, 'device_name': deviceName});
  }

  Future<void> deletePushToken(String token) async {
    await _dio.delete('/api/push/register', data: {'token': token});
  }

  /// Set the access token for subsequent authenticated requests.
  void setAccessToken(String token) {
    _dio.options.headers['Authorization'] = 'Bearer $token';
  }

  void clearAuth() {
    _dio.options.headers.remove('Authorization');
  }
}

/// Intercepts 401 responses, refreshes the access token, and retries once.
class _JwtInterceptor extends Interceptor {
  final Dio _dio;
  final String _baseUrl;
  bool _refreshing = false;

  _JwtInterceptor(this._dio, this._baseUrl);

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) async {
    final tokens = await storage.loadTokens();
    if (tokens.accessToken != null) {
      options.headers['Authorization'] = 'Bearer ${tokens.accessToken}';
    }
    handler.next(options);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) async {
    if (err.response?.statusCode != 401 || _refreshing) {
      return handler.next(err);
    }
    _refreshing = true;
    try {
      final tokens = await storage.loadTokens();
      if (tokens.refreshToken == null) {
        return handler.next(err);
      }
      final refreshDio = Dio(BaseOptions(baseUrl: _baseUrl));
      final r = await refreshDio.post<Map<String, dynamic>>('/api/auth/refresh',
          data: {'refresh_token': tokens.refreshToken});
      final newAccess = r.data!['access_token'] as String;
      await storage.saveTokens(
        accessToken:  newAccess,
        refreshToken: tokens.refreshToken!,
        userID:       tokens.userID ?? '',
      );
      // Retry original request with new token.
      final opts = err.requestOptions;
      opts.headers['Authorization'] = 'Bearer $newAccess';
      final retryResp = await _dio.request<dynamic>(
        opts.path,
        data: opts.data,
        queryParameters: opts.queryParameters,
        options: Options(method: opts.method, headers: opts.headers),
      );
      handler.resolve(retryResp);
    } catch (_) {
      handler.next(err);
    } finally {
      _refreshing = false;
    }
  }
}
