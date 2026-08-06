import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_connector.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/domain/tray_presence.dart';
import 'package:kanpachi_ui/features/shell/infra/windows_tray.dart';
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

    // El daemon de verdad, y no hay otra opción a propósito.
    //
    // Hubo una implementación falsa mientras el named pipe no existía. Ya no
    // existe: dejar un camino que inventa salas es dejar una forma de que la
    // app se vea perfecta sin que nadie haya hablado con el daemon, y eso es lo
    // contrario de lo que este producto tiene que poder demostrar de sí mismo.
    i.registerLazySingleton<SessionRepository>(
      () => PipeSessionRepository(DaemonConnector()),
    );

    // lazySingleton y no factory, a diferencia del resto de cubits de la casa:
    // seis pantallas leen y escriben la misma sala, y una instancia por
    // pantalla serían seis copias divergentes de un estado que es uno solo.
    i.registerLazySingleton<SessionCubit>(
      () => SessionCubit(i.get<SessionRepository>()),
    );
  }

  static void _registerShell() {
    final Injector i = Injector.instance;
    i.registerLazySingleton<ShellCubit>(ShellCubit.new);
    // Detrás del contrato para que los tests de widget no toquen la bandeja
    // del sistema: un test que planta un icono de verdad lo deja puesto
    // cuando falla.
    i.registerLazySingleton<TrayPresence>(WindowsTray.new);
  }
}
