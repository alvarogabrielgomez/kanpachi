// lib/integrations/packages/dio/dio_http_helper.dart

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

import '../../../ioc/injector.dart';
import '../../environments/environment_config.dart';
import '../../environments/remote_service.dart';
import '../../helpers/http_helper.dart';

/// Un ÚNICO Dio para toda la app — los interceptors viven acá.
///
/// La base URL por-request se elige con [withBaseUrl], que muta
/// `dio.options.baseUrl` y devuelve un [HttpHelper] listo.
class DioHttpHelper {
  DioHttpHelper(this._authInterceptor) {
    _setup();
  }

  late Dio _dio;
  final Interceptor _authInterceptor;

  /// Expuesto SÓLO para tooling de debug (el inspector Alice), que necesita
  /// engancharse a la misma instancia. Producción usa [withBaseUrl].
  Dio get dio => _dio;

  void _setup() {
    _dio = Dio(
      BaseOptions(
        sendTimeout: EnvironmentConfig.httpTimeout,
        receiveTimeout: EnvironmentConfig.httpTimeout,
        connectTimeout: EnvironmentConfig.httpTimeout,
        contentType: 'application/json; charset=utf-8',
        // ⚠ NO TOCAR sin leer el porqué.
        // 401 DEBE levantar DioException para que AuthInterceptor.onError
        // dispare el refresh-and-retry. Cualquier otro 4xx pasa por el success
        // path para que el body parser de HttpHelper lo mapee
        // (400→BadRequest, 403→Forbidden, 409→Conflict…).
        // Si 401 entrara acá, onError nunca correría: el usuario quedaría
        // deslogueado al primer access token vencido.
        validateStatus: (s) => s != null && s != 401 && s < 500,
      ),
    );

    _dio.interceptors.add(_authInterceptor);

    if (kDebugMode) {
      _dio.interceptors.add(
        LogInterceptor(
          request: false,
          requestHeader: false,
          requestBody: false,
          responseHeader: false,
          responseBody: false,
          error: true,
        ),
      );
    }
  }

  HttpHelper withBaseUrl(RemoteService service) {
    final urls = Injector.instance.resolve<Map<RemoteService, String>>();
    _dio.options.baseUrl = urls[service]!;
    return HttpHelper(_dio);
  }
}
