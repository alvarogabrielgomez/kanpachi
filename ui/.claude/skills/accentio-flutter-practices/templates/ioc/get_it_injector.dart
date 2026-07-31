// lib/integrations/packages/get_it/get_it_injector.dart

import 'package:get_it/get_it.dart';

import '../../../ioc/injector.dart';

/// Implementación de [Injector] sobre get_it.
///
/// **Desregistra antes de registrar** en todos los métodos: sin eso, un hot
/// reload o un `IocManager.register()` repetido en tests explota con
/// "already registered".
class GetItInjector implements Injector {
  final GetIt _getIt = GetIt.instance;

  @override
  void registerFactory<T extends Object>(FactoryFunc<T> f) {
    if (_getIt.isRegistered<T>()) _getIt.unregister<T>();
    _getIt.registerFactory<T>(f);
  }

  @override
  void registerLazySingleton<T extends Object>(FactoryFunc<T> f) {
    if (_getIt.isRegistered<T>()) _getIt.unregister<T>();
    _getIt.registerLazySingleton<T>(f);
  }

  @override
  void registerSingleton<T extends Object>(T value) {
    if (_getIt.isRegistered<T>()) _getIt.unregister<T>();
    _getIt.registerSingleton<T>(value);
  }

  @override
  void registerLazySingletonByName<T extends Object>(
    FactoryFunc<T> f,
    String name,
  ) {
    if (_getIt.isRegistered<T>(instanceName: name)) {
      _getIt.unregister<T>(instanceName: name);
    }
    _getIt.registerLazySingleton<T>(f, instanceName: name);
  }

  @override
  T resolve<T extends Object>() => _getIt.get<T>();

  @override
  T resolveByName<T extends Object>(String name) => _getIt.get<T>(instanceName: name);

  @override
  bool isRegistered<T extends Object>() => _getIt.isRegistered<T>();
}
