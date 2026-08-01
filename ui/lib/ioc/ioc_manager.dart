import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/infra/fake_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/ioc/injector.dart';

/// El registro de dependencias, en un solo sitio y con orden explícito.
///
/// Corre una vez en `main()`. El orden importa: lo que depende de otro va
/// después.
abstract final class IocManager {
  static void register({Injector? injector}) {
    Injector.instance = injector ?? GetItInjector();
    _registerSession();
    _registerShell();
  }

  static void _registerSession() {
    final Injector i = Injector.instance;

    // Hoy es la implementación falsa porque el daemon todavía no existe. El
    // día que exista, esta línea es lo ÚNICO que cambia: ni las pantallas ni
    // los cubits conocen otra cosa que el contrato.
    i.registerLazySingleton<SessionRepository>(FakeSessionRepository.new);

    // lazySingleton y no factory, a diferencia del resto de cubits de la casa:
    // seis pantallas leen y escriben la misma sala, y una instancia por
    // pantalla serían seis copias divergentes de un estado que es uno solo.
    i.registerLazySingleton<SessionCubit>(
      () => SessionCubit(i.get<SessionRepository>()),
    );
  }

  static void _registerShell() {
    Injector.instance.registerLazySingleton<ShellCubit>(ShellCubit.new);
  }
}
