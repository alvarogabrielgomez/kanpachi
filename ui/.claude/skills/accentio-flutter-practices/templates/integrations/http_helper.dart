// lib/integrations/helpers/http_helper.dart
//
// Verbos HTTP + la ÚNICA tabla status→Failure del proyecto.

import 'package:dio/dio.dart';

import '../../core/failures/app_error_code.dart';
import '../../core/failures/failure.dart';

class HttpResponse {
  HttpResponse({this.statusCode, this.data});

  final int? statusCode;
  final Object? data;

  bool isSuccess() =>
      statusCode != null && statusCode! >= 200 && statusCode! < 300;
}

/// Wrapper delgado sobre un Dio ya configurado. Los repos lo obtienen vía el
/// mixin `Api` y llaman los verbos.
///
/// Los verbos NUNCA devuelven el error crudo de Dio: o devuelven [HttpResponse]
/// (2xx, o un 4xx que `validateStatus` dejó pasar) o lanzan un [Failure]
/// tipado. Corolario para los callers: **si el await retornó, no hubo error de
/// transporte** — no chequear `isSuccess()` para detectar fallos.
class HttpHelper {
  HttpHelper(this._dio);

  final Dio _dio;

  Future<HttpResponse> get(String path, {Map<String, dynamic>? query}) async {
    try {
      final r = await _dio.get(path, queryParameters: query);
      return HttpResponse(statusCode: r.statusCode, data: r.data);
    } on DioException catch (e) {
      _rethrowAsFailure(e);
    }
  }

  Future<HttpResponse> post(
    String path, {
    Object? data,
    Map<String, dynamic>? headers,
  }) async {
    try {
      final r = await _dio.post(
        path,
        data: data,
        options: headers == null ? null : Options(headers: headers),
      );
      return HttpResponse(statusCode: r.statusCode, data: r.data);
    } on DioException catch (e) {
      _rethrowAsFailure(e);
    }
  }

  Future<HttpResponse> patch(String path, {Object? data}) async {
    try {
      final r = await _dio.patch(path, data: data);
      return HttpResponse(statusCode: r.statusCode, data: r.data);
    } on DioException catch (e) {
      _rethrowAsFailure(e);
    }
  }

  Future<HttpResponse> put(String path, {Object? data}) async {
    try {
      final r = await _dio.put(path, data: data);
      return HttpResponse(statusCode: r.statusCode, data: r.data);
    } on DioException catch (e) {
      _rethrowAsFailure(e);
    }
  }

  Future<HttpResponse> delete(String path, {Object? data}) async {
    try {
      final r = await _dio.delete(path, data: data);
      return HttpResponse(statusCode: r.statusCode, data: r.data);
    } on DioException catch (e) {
      _rethrowAsFailure(e);
    }
  }

  Never _rethrowAsFailure(DioException e) {
    // Ya tipado por un interceptor (p. ej. el refresh que murió).
    if (e.error is Failure) throw e.error as Failure;

    if (e.type == DioExceptionType.connectionTimeout ||
        e.type == DioExceptionType.receiveTimeout ||
        e.type == DioExceptionType.sendTimeout ||
        e.type == DioExceptionType.connectionError) {
      throw NetworkFailure();
    }

    // DioException CON response (el 401 que validateStatus excluyó) → misma
    // tabla que usa el success path de handle().
    final response = e.response;
    if (response != null) {
      checkFailures(
        HttpResponse(statusCode: response.statusCode, data: response.data),
      );
    }
    throw CommonFailure(e.message);
  }
}

typedef HttpResponseDataMapper<T> = T Function(Object data);

/// `Future<HttpResponse>` → `Stream<T>` que emite UNA vez o lanza un [Failure].
///
/// Convención de repo: `yield* api.get(path).handle(mapper: ...)`.
extension FutureHttpExtension on Future<HttpResponse> {
  Stream<T> handle<T>({required HttpResponseDataMapper<T> mapper}) async* {
    final response = await this;

    if (response.isSuccess() && response.data != null) {
      try {
        yield mapper(response.data!);
        return;
      } catch (e) {
        // El mapper explotó parseando: no dejar escapar el error crudo.
        throw JsonParseFailure(error: e);
      }
    }
    if (response.isSuccess() && response.data == null) {
      // Señal de que el endpoint quería handleVoid().
      throw CommonFailure('Empty response body');
    }
    checkFailures(response);
  }
}

/// Variante para 204 / respuestas sin body.
extension FutureHttpExtensionVoid on Future<HttpResponse> {
  Stream<void> handleVoid() async* {
    final response = await this;
    if (response.isSuccess()) {
      yield null;
      return;
    }
    checkFailures(response);
  }
}

/// **El único traductor status→Failure.** Lo comparten `handle()` y
/// `_rethrowAsFailure`, así el código de app se adjunta igual venga por donde
/// venga el error.
Never checkFailures(HttpResponse response) {
  final status = response.data;
  final asMap = status is Map<String, dynamic> ? status : null;
  final code = response.statusCode;
  final message = asMap == null
      ? null
      : (asMap['message']?.toString() ?? asMap['error']?.toString());
  final appCode = _parseAppCode(asMap?['code']);
  final data = asMap?['data'];

  // 1) Subclases tipadas con metadata — SIEMPRE antes del switch por status.
  //    Acá también es donde se encienden las señales globales (una pantalla
  //    que tiene que ganar), para que un caller que se trague el throw no
  //    pueda ocultar el evento.
  if (appCode == AppErrorCode.dataConflict && data is Map<String, dynamic>) {
    throw DataConflictFailure(
      serverVersion: (data['serverVersion'] as num?)?.toInt(),
      message: message,
    );
  }

  // 2) Switch por status.
  switch (code) {
    case 400:
      throw BadRequestFailure(message, appCode);
    case 401:
      throw UnauthorizedFailure(appCode);
    case 403:
      throw ForbiddenFailure(appCode);
    case 404:
      throw NotFoundFailure();
    case 409:
      throw ConflictFailure(message, appCode);
    case 426:
      throw BadRequestFailure(message, appCode); // Upgrade Required
    default:
      throw CommonFailure('HTTP $code${message == null ? '' : ' — $message'}');
  }
}

/// Tolerante a int / num / String: el backend puede serializar de las tres.
int? _parseAppCode(Object? raw) {
  if (raw is int) return raw;
  if (raw is num) return raw.toInt();
  if (raw is String) return int.tryParse(raw);
  return null;
}
