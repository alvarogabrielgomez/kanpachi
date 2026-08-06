import 'dart:io';
import 'package:flutter/foundation.dart';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/platform/app_preferences.dart';
import 'package:kanpachi_ui/core/platform/single_instance.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/tray_bridge.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/window_size_memory.dart';
import 'package:kanpachi_ui/ioc/injector.dart';
import 'package:kanpachi_ui/ioc/ioc_manager.dart';
import 'package:window_manager/window_manager.dart';

/// La bandera con la que el daemon pide abrir la ventana.
///
/// **Sin ella, Kanpachi arranca callado: bandeja y nada más.** El silencio es
/// el default a propósito. Una bandera que se pierda por el camino tiene que
/// fallar hacia el lado callado; al revés, el fallo es una ventana abriéndose
/// sola encima de lo que estuvieras haciendo al encender la PC.
///
/// Es un CONTRATO con `daemon/cmd/kanpachid`, que la escribe en su propia
/// constante `uiShowFlag`. De los que no dan error al romperse: cambiar una sin
/// la otra produce una interfaz que no se abre nunca.
const String kShowFlag = '--show';

Future<void> main(List<String> args) async {
  WidgetsFlutterBinding.ensureInitialized();

  // **Antes que nada.** Si ya hay un Kanpachi corriendo, este proceso solo
  // existe para avisarle, y eso ya lo hizo `claim`. Seguir adelante abriría una
  // segunda ventana y un segundo icono en la bandeja.
  //
  // Pasa de verdad y no es un caso raro: es cómo el daemon abre la ventana
  // cuando alguien hace doble clic en el acceso directo con Kanpachi ya
  // corriendo, y es cómo va a entrar un enlace `kanpachi://`.
  final bool onlyOne = SingleInstance.claim(onShowRequested: _traerAlFrente);
  if (!onlyOne) {
    exit(0);
  }

  // What this window remembers between runs, read BEFORE the first frame.
  //
  // It has to be read here and not later: it decides which screen opens. Read
  // after the first frame, onboarding would flash and then disappear, which is
  // worse than showing it.
  final AppPreferences preferences = await AppPreferences.open();

  IocManager.register(preferences: preferences);
  await _prepareWindow(
    silent: await _shouldStayQuiet(args),
    size: preferences.windowSize ?? AppSpacing.initialWindow,
  );
  runApp(
    KanpachiApp(onboarded: preferences.onboarded, preferences: preferences),
  );
}

/// Whether this run should stay in the tray with no window.
///
/// Silence is the default of both executables, and the flag asks to SHOW. That
/// direction is deliberate: a flag lost along the way has to fail towards
/// quiet, because the other way round the failure is a window opening by itself
/// on top of whatever you were doing when the machine booted.
///
/// Two things override it, and both are the same idea — silence only makes
/// sense when something else is carrying the tray icon.
///
///  - **No daemon.** The icon only exists if there is one, so a hidden window
///    with no daemon is a process with no window, no icon, and no way to close
///    it short of Task Manager. See [_hayDaemon].
///  - **A debug build.** `flutter run` cannot pass arguments to the app, so
///    every development run would come up invisible. A debug build is a
///    development session by definition, and there is no daemon parenting it.
///    Silent startup is measured with the release binary, which is the one that
///    ships.
Future<bool> _shouldStayQuiet(List<String> args) async {
  if (kDebugMode) return false;
  if (args.contains(kShowFlag)) return false;
  return _hayDaemon();
}

/// Si hay un daemon del otro lado del pipe, ahora mismo.
///
/// # Por qué el arranque silencioso lo pregunta
///
/// Porque **el silencio significa "mi cara es el icono de la bandeja"**, y ese
/// icono solo existe si hay daemon: es la invariante de `docs/03`. Sin daemon
/// no hay bandeja, así que una ventana escondida sería un proceso sin ventana,
/// sin icono y sin forma de cerrarlo que no sea el Administrador de tareas.
///
/// Pasa por una puerta concreta: alguien entra a la carpeta de Kanpachi y abre
/// este ejecutable a mano. Es legítimo, y lo que tiene que ver entonces es el
/// aviso de que el servicio no está corriendo, o sea los mandos diciendo que no
/// hay nada que mandar.
///
/// No cuesta esperar: sin daemon, abrir el named pipe falla en el acto con
/// ERROR_FILE_NOT_FOUND, que es justo el caso que decide esto.
///
/// **Pregunta por el catálogo y NO por la salud, y eso importa.** `health()` se
/// traga cualquier fallo y devuelve un informe desconocido, que es lo correcto
/// para una pantalla y lo inservible para esto: contestaría que sí siempre.
/// Medido — la primera versión preguntaba por la salud y la ventana no se abría
/// nunca. `catalog()` deja pasar el error, y de paso lo que traiga queda
/// cacheado para la pantalla que lo va a pedir enseguida.
Future<bool> _hayDaemon() async {
  try {
    await Injector.instance.get<SessionRepository>().catalog();
    return true;
  } on Object {
    // Cualquier fallo cuenta como que no hay: el daemon que no contesta y el
    // que no está son lo mismo para quien está mirando la pantalla.
    return false;
  }
}

/// Kanpachi dibuja su propia barra de título, así que la del sistema se
/// esconde.
///
/// No es estética: esa barra es donde viven el nombre con el que te ven y el
/// aviso de que cerrar la ventana NO cierra la sala. Con el marco nativo
/// encima habría dos barras de título, dos juegos de botones de ventana y dos
/// respuestas distintas a la misma cruz.
///
/// # El arranque silencioso
///
/// Con `silent`, la ventana se prepara y NO se enseña. Es como entra Kanpachi
/// cuando Windows levanta el servicio al encender la PC: aparece el icono de la
/// bandeja y nada más. Abrir una ventana encima de lo que alguien estuviera
/// haciendo al arrancar sesión es exactamente lo que un programa que se instala
/// una vez no debe hacer.
///
/// La ventana se construye igual, y eso es a propósito: así el catálogo se pide
/// y la salud se mide desde el primer momento, y el menú de la bandeja tiene
/// algo que decir antes de que nadie la abra.
Future<void> _prepareWindow({required bool silent, required Size size}) async {
  await windowManager.ensureInitialized();
  final WindowOptions options = WindowOptions(
    size: size,
    minimumSize: AppSpacing.minWindow,
    // **Centrada SIEMPRE, y la posición no se guarda.** El tamaño sí, y no son
    // la misma decisión: quien agrandó la ventana la quiere grande otra vez, y
    // dónde estaba era cosa de aquel momento. Recordar la posición es cómo una
    // ventana vuelve medio fuera de la pantalla al desenchufar un monitor.
    center: true,
    backgroundColor: Colors.transparent,
    // Fuera de la barra de tareas mientras esté escondida: una entrada en la
    // barra sin ventana detrás es un botón que no hace nada.
    skipTaskbar: silent,
    titleBarStyle: TitleBarStyle.hidden,
    windowButtonVisibility: false,
    title: 'Kanpachi',
  );
  await windowManager.waitUntilReadyToShow(options, () async {
    // **Centrada a mano, además de pedirlo en las opciones.**
    //
    // `WindowOptions.center` no es fiable, y no es una sospecha: medido dos
    // veces seguidas con la misma compilación, una abrió centrada en el
    // monitor principal, en (268,130), y la otra en (2450,377), que no es el
    // centro de ninguno de los dos monitores de esta máquina. Se centra otra
    // vez acá, que es después de aplicar las opciones y con el tamaño ya
    // puesto.
    await windowManager.center();
    if (silent) return;
    await windowManager.show();
    await windowManager.focus();
  });

  // **Esconderla explícitamente, y DESPUÉS de aplicar las opciones.**
  //
  // No basta con no llamar a `show()`. El runner de Flutter crea la ventana con
  // `WS_OVERLAPPEDWINDOW`, que NO trae `WS_VISIBLE`, o sea que nace oculta; lo
  // que la enseña es el propio `window_manager` al aplicar las opciones, porque
  // quitar la barra de título rehace el marco y eso la levanta.
  //
  // Medido: en silencio y solo omitiendo `show()`, `IsWindowVisible`
  // contestaba `true`. Un test no lo habría visto — hay que arrancar el
  // ejecutable y preguntarle a Windows si la ventana está en pantalla.
  if (silent) {
    await windowManager.hide();
  }
}

/// Traer la ventana al frente cuando otro proceso lo pide.
///
/// `show()` sola no basta si quedó minimizada, y tampoco si arrancó silenciosa
/// con `skipTaskbar`: hay que restaurarla, devolverla a la barra de tareas, y
/// enfocarla. Sin el foco reaparece detrás de lo que el usuario tenga delante,
/// que para él es lo mismo que no reaparecer.
Future<void> _traerAlFrente() async {
  if (await windowManager.isMinimized()) await windowManager.restore();
  await windowManager.setSkipTaskbar(false);
  await windowManager.show();
  await windowManager.focus();
}

class KanpachiApp extends StatelessWidget {
  const KanpachiApp({this.onboarded = false, this.preferences, super.key});

  /// Whether this machine already went through onboarding.
  ///
  /// Which is the same question as having a nickname: onboarding is two
  /// screens and the second one asks for it. See [AppPreferences].
  final bool onboarded;

  /// Where this window remembers things. Null in tests, which have no window.
  final AppPreferences? preferences;

  @override
  Widget build(BuildContext context) {
    return MultiBlocProvider(
      providers: <BlocProvider<dynamic>>[
        BlocProvider<ShellCubit>(
          // Straight to the home screen when there is already a name. Asking
          // again on every start was not just noise: the answer was thrown
          // away each time, so the name never survived a restart and the
          // daemon rejects opening a room without one.
          create: (_) =>
              Injector.instance.get<ShellCubit>()
                ..go(onboarded ? AppScreen.home : AppScreen.welcome),
        ),
        BlocProvider<SessionCubit>(
          // El latido, y con él todo lo demás: la sala, la salud y el catálogo.
          //
          // Antes eran dos llamadas sueltas al arrancar la ventana, y esa era
          // toda la vida del estado: lo que el daemon hiciera después no
          // llegaba nunca. Ver [SessionCubit.watchSession].
          create: (_) => Injector.instance.get<SessionCubit>()..watchSession(),
        ),
      ],
      // Por encima de la app y por debajo de los cubits: tiene que durar lo
      // que dure la ventana y necesita leer la sala para escribir el menú de
      // la bandeja.
      child: TrayBridge(
        child: WindowSizeMemory(
          preferences: preferences,
          child: const _ThemedApp(),
        ),
      ),
    );
  }
}

/// El tema se reconstruye cuando cambian el modo o la densidad, así que va en
/// su propio widget: si viviera dentro de [KanpachiApp], cada cambio de
/// densidad reconstruiría también los providers.
class _ThemedApp extends StatelessWidget {
  const _ThemedApp();

  @override
  Widget build(BuildContext context) {
    final ShellState shell = context.watch<ShellCubit>().state;
    final DensityTokens density = DensityTokens.of(shell.density);

    return MaterialApp(
      title: 'Kanpachi',
      debugShowCheckedModeBanner: false,
      themeMode: shell.themeMode,
      theme: AppTheme.light(density: density),
      darkTheme: AppTheme.dark(density: density),
      home: const ShellPage(),
    );
  }
}
