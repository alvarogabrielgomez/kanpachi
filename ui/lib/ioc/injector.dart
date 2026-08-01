import 'package:get_it/get_it.dart';

/// La abstracción sobre el contenedor.
///
/// Existe para que cambiar de contenedor sea tocar un archivo y no doscientos
/// `GetIt.I<...>` repartidos por las features. Nada resuelve dependencias
/// fuera de acá.
abstract interface class Injector {
  static late Injector instance;

  T get<T extends Object>();

  /// Una sola instancia, construida la primera vez que alguien la pide.
  void registerLazySingleton<T extends Object>(T Function() factory);

  /// Una instancia nueva por cada quien la pida: los cubits de pantalla.
  void registerFactory<T extends Object>(T Function() factory);

  /// Ya construida antes de registrar.
  void registerSingleton<T extends Object>(T value);
}

class GetItInjector implements Injector {
  GetItInjector([GetIt? getIt]) : _getIt = getIt ?? GetIt.instance;

  final GetIt _getIt;

  @override
  T get<T extends Object>() => _getIt<T>();

  /// Desregistra antes de registrar, siempre.
  ///
  /// Sin esto, un hot reload vuelve a correr el registro sobre un contenedor
  /// que ya tiene esos tipos y explota; y en tests, el segundo `setUp` que
  /// registra lo mismo también. Los dos son escenarios normales, no rarezas.
  void _clear<T extends Object>() {
    if (_getIt.isRegistered<T>()) _getIt.unregister<T>();
  }

  @override
  void registerLazySingleton<T extends Object>(T Function() factory) {
    _clear<T>();
    _getIt.registerLazySingleton<T>(factory);
  }

  @override
  void registerFactory<T extends Object>(T Function() factory) {
    _clear<T>();
    _getIt.registerFactory<T>(factory);
  }

  @override
  void registerSingleton<T extends Object>(T value) {
    _clear<T>();
    _getIt.registerSingleton<T>(value);
  }
}
