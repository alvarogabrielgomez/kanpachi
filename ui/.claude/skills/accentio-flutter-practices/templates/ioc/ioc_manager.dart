// lib/ioc/ioc_manager.dart
//
// Registro central. Se llama UNA vez desde main(), después de inicializar la DB
// local. El ORDEN importa: lo que depende de otro va después.

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../integrations/environments/environment_config.dart';
import '../integrations/environments/remote_service.dart';
import '../integrations/packages/dio/dio_http_helper.dart';
import '../integrations/packages/get_it/get_it_injector.dart';
import 'injector.dart';

abstract class IocManager {
  static Future<void> register(/* AppDatabase db */) async {
    final i = Injector.register(GetItInjector());

    // ── Globals ────────────────────────────────────────────────────────────
    // i.registerSingleton<AppDatabase>(db);
    i.registerSingleton<Map<RemoteService, String>>(
      EnvironmentConfig.environmentUrl,
    );
    i.registerLazySingleton<FlutterSecureStorage>(
      () => const FlutterSecureStorage(),
    );

    _registerAuth(i); // storage → interceptor → repo → use cases → cubit
    _registerHttp(i); // DioHttpHelper(AuthInterceptor)  ← DESPUÉS de auth
    // _registerFeatureX(i);
  }

  /// Patrón por feature, sin excepciones:
  ///   repository → lazySingleton   (sin estado por-request)
  ///   use cases  → lazySingleton
  ///   cubit      → factory         (uno por pantalla)
  static void _registerAuth(Injector i) {
    // i.registerLazySingleton<AuthStorage>(
    //   () => AuthStorage(i.resolve<FlutterSecureStorage>()),
    // );
    // i.registerLazySingleton<AuthInterceptor>(
    //   () => AuthInterceptor(i.resolve<AuthStorage>()),
    // );
    // i.registerLazySingleton<AuthRepository>(() => AuthRemoteRepository());
    // i.registerLazySingleton<AuthUseCases>(
    //   () => AuthUseCasesImpl(
    //     repository: i.resolve<AuthRepository>(),
    //     storage: i.resolve<AuthStorage>(),
    //   ),
    // );
    // i.registerFactory<AuthCubit>(() => AuthCubit(i.resolve<AuthUseCases>()));
  }

  static void _registerHttp(Injector i) {
    // El interceptor ya existe acá — por eso este bloque corre después.
    // i.registerSingleton<DioHttpHelper>(
    //   DioHttpHelper(i.resolve<AuthInterceptor>()),
    // );
  }
}
