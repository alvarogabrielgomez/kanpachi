// lib/features/auth/infra/auth_interceptor.dart

import 'package:dio/dio.dart';

import '../../../core/failures/app_error_code.dart';
import '../../../core/failures/failure.dart';
import '../../../ioc/injector.dart';

/// Bearer en el request + refresh single-flight ante 401 refrescable.
class AuthInterceptor extends Interceptor {
  AuthInterceptor(this._storage);

  final AuthStorage _storage;

  Future<void>? _inflightRefresh;

  /// Rutas que NO llevan bearer y NO disparan refresh.
  static const _publicPaths = <String>{
    '/auth/login',
    '/auth/refresh',
    '/auth/register',
    '/auth/logout',
    '/auth/forgot-password',
    '/auth/verify-otp',
    '/auth/reset-password',
  };

  bool _isPublic(String path) {
    // Limpiar query string y host antes de matchear.
    final clean = Uri.parse(path).path;
    if (_publicPaths.contains(clean)) return true;
    // Patrones dinámicos (p. ej. lookups pre-login):
    // if (RegExp(r'^/tenants/[^/]+/branding$').hasMatch(clean)) return true;
    return false;
  }

  @override
  Future<void> onRequest(
    RequestOptions options,
    RequestInterceptorHandler handler,
  ) async {
    if (!_isPublic(options.path)) {
      final token = await _storage.readAccessToken();
      if (token != null) {
        options.headers['Authorization'] = 'Bearer $token';
      }
    }
    handler.next(options);
  }

  @override
  Future<void> onError(
    DioException err,
    ErrorInterceptorHandler handler,
  ) async {
    final response = err.response;
    if (response?.statusCode != 401 || _isPublic(err.requestOptions.path)) {
      return handler.next(err);
    }

    // El 401 está sobrecargado server-side. Sólo refrescar cuando el backend
    // dice que el problema es el ACCESS token.
    final authCode = _extractAuthCode(response);
    if (authCode == null || !AppErrorCode.refreshable.contains(authCode)) {
      return handler.next(err);
    }

    try {
      // SINGLE-FLIGHT: un burst de 401 paralelos dispara un solo refresh.
      _inflightRefresh ??= _doRefresh().whenComplete(
        () => _inflightRefresh = null,
      );
      await _inflightRefresh;
    } on UnauthorizedFailure {
      // El refresh token está REALMENTE muerto → dropear la sesión.
      await _storage.clear();
      return handler.reject(
        DioException(
          requestOptions: err.requestOptions,
          error: UnauthorizedFailure(authCode),
        ),
      );
    } catch (e) {
      // Fallo TRANSIENTE (timeout / 5xx / DNS): NO nukear la sesión. Sin este
      // guard, un tick de red flaky desloguea a un usuario con credenciales
      // válidas.
      return handler.reject(err);
    }

    // Retry del request original con un Dio FRESCO, sin este interceptor
    // (anti-recursión).
    final token = await _storage.readAccessToken();
    final retryOptions = err.requestOptions
      ..headers['Authorization'] = 'Bearer $token';

    final retryDio = Dio()
      ..options = BaseOptions(validateStatus: (s) => s != null && s < 500);

    try {
      final retried = await retryDio.fetch(retryOptions);
      return handler.resolve(retried);
    } on DioException catch (e) {
      return handler.reject(e);
    }
  }

  /// Resuelve el repo LAZY: el interceptor se registra antes que el repo, así
  /// que capturarlo en el constructor sería una dependencia circular.
  Future<void> _doRefresh() async {
    final repo = Injector.instance.resolve<AuthRepository>();
    final current = await _storage.readRefreshToken();
    if (current == null) throw UnauthorizedFailure(AppErrorCode.noAuth);
    final tokens = await repo.refresh(current).first;
    await _storage.writeTokens(tokens);
  }

  int? _extractAuthCode(Response? response) {
    final data = response?.data;
    if (data is Map<String, dynamic>) {
      final raw = data['code'];
      if (raw is int) return raw;
      if (raw is num) return raw.toInt();
      if (raw is String) return int.tryParse(raw);
    }
    return null;
  }
}

// Contratos que este archivo asume (definilos en tu feature de auth):
//
// abstract class AuthStorage {
//   Future<String?> readAccessToken();
//   Future<String?> readRefreshToken();
//   Future<void> writeTokens(Object tokens);
//   Future<void> clear();
// }
//
// abstract class AuthRepository {
//   Stream<Object> refresh(String refreshToken);
// }
