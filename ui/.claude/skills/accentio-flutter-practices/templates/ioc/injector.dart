// lib/ioc/injector.dart
//
// Abstracción del contenedor de DI. Todo el proyecto resuelve por acá, así que
// cambiar de librería (get_it → otra) toca un solo archivo.

typedef FactoryFunc<T> = T Function();

abstract class Injector {
  static late Injector instance;

  static Injector register(Injector impl) {
    instance = impl;
    return impl;
  }

  void registerFactory<T extends Object>(FactoryFunc<T> f);
  void registerLazySingleton<T extends Object>(FactoryFunc<T> f);
  void registerSingleton<T extends Object>(T value);
  void registerLazySingletonByName<T extends Object>(FactoryFunc<T> f, String name);

  T resolve<T extends Object>();
  T resolveByName<T extends Object>(String name);

  /// Permite que código compartido entre flavors pregunte si un servicio existe
  /// en ESTE binario, sin branchear por flavor.
  bool isRegistered<T extends Object>();
}
