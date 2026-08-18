import 'package:kanpachi_ui/core/platform/app_preferences.dart';
import 'package:kanpachi_ui/core/platform/machine_profile.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_connector.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/domain/tray_presence.dart';
import 'package:kanpachi_ui/features/shell/infra/windows_tray.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/update/domain/repositories/update_repository.dart';
import 'package:kanpachi_ui/features/update/infra/github_update_repository.dart';
import 'package:kanpachi_ui/features/update/presentation/cubit/update_cubit.dart';
import 'package:kanpachi_ui/ioc/injector.dart';

/// El registro de dependencias, en un solo sitio y con orden explícito.
///
/// Corre una vez en `main()`. El orden importa: lo que depende de otro va
/// después.
abstract final class IocManager {
  /// `portable` lo resuelve `main()` y llega hasta acá en vez de leerse abajo:
  /// la respuesta vive en un marcador en disco que conoce infra, y la capa de
  /// presentación no puede importar infra. Ver [ShellState.portable].
  static void register({
    Injector? injector,
    AppPreferences? preferences,
    bool portable = false,

    /// Si el daemon dijo, en la línea de comandos, que está reabriendo la sala
    /// del arranque anterior. Ver `kResumeHostedRoomFlag` en `main.dart`.
    bool resumingHostedRoom = false,

    /// El nombre que el daemon guarda, ya leído del disco. Ver [MachineProfile].
    MachineProfile? profile,
  }) {
    Injector.instance = injector ?? GetItInjector();
    if (preferences != null) {
      Injector.instance.registerLazySingleton<AppPreferences>(
        () => preferences,
      );
    }
    _registerSession(preferences, profile, resumingHostedRoom);
    _registerShell(portable);
    _registerUpdate(preferences);
  }

  static void _registerSession(
    AppPreferences? preferences,
    MachineProfile? profile,
    bool resumingHostedRoom,
  ) {
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
      () => SessionCubit(
        i.get<SessionRepository>(),
        preferences: preferences,
        // Del perfil que escribe el DAEMON, no de los ajustes de esta ventana:
        // el nombre es uno por máquina. Ver [MachineProfile].
        nickname: profile?.nickname ?? '',
        // Off when there is nowhere to have stored it, which is the tests.
        // The stored default already leans on the build kind; see
        // [AppPreferences.verbose].
        verbose: preferences?.verbose ?? false,
        resumingHostedRoom: resumingHostedRoom,
      ),
    );
  }

  static void _registerUpdate(AppPreferences? preferences) {
    final Injector i = Injector.instance;
    i.registerLazySingleton<UpdateRepository>(GithubUpdateRepository.new);
    // lazySingleton por el mismo motivo que la sesión: lo que sabe —que hay
    // una versión nueva— es un hecho de la aplicación, no de una pantalla, y
    // dos instancias serían dos respuestas a la misma pregunta, cada una
    // gastando su propia petición.
    i.registerLazySingleton<UpdateCubit>(
      () => UpdateCubit(i.get<UpdateRepository>(), preferences: preferences),
    );
  }

  static void _registerShell(bool portable) {
    final Injector i = Injector.instance;
    i.registerLazySingleton<ShellCubit>(() => ShellCubit(portable: portable));
    // Detrás del contrato para que los tests de widget no toquen la bandeja
    // del sistema: un test que planta un icono de verdad lo deja puesto
    // cuando falla.
    i.registerLazySingleton<TrayPresence>(WindowsTray.new);
  }
}
