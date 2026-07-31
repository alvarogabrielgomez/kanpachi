// lib/integrations/environments/remote_service.dart
//
//   /// Un valor por microservicio. Arranca con uno; se agregan entradas si la
//   /// API se parte.
//   enum RemoteService { appApi }

// lib/integrations/environments/environment_config.dart

import 'remote_service.dart';

/// Nombres de las claves de compile-time. Aparte para que el `--dart-define`
/// y el `String.fromEnvironment` no puedan divergir en un typo.
abstract class EnvironmentKey {
  static const String apiBaseUrl = 'API_BASE_URL';
}

abstract class EnvironmentConfig {
  /// Base URL por `--dart-define=API_BASE_URL=…`. El default apunta a dev.
  static const String apiBaseUrl = String.fromEnvironment(
    EnvironmentKey.apiBaseUrl,
    defaultValue: 'https://api.example.dev/api/',
  );

  static const Duration httpTimeout = Duration(seconds: 30);

  /// Se registra como singleton en el IoC; `DioHttpHelper` lo resuelve para
  /// mapear servicio → URL.
  static Map<RemoteService, String> get environmentUrl => {
    RemoteService.appApi: apiBaseUrl,
  };
}
