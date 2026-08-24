import 'dart:async';
import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter/gestures.dart';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/platform/app_log.dart';
import 'package:kanpachi_ui/core/platform/app_preferences.dart';
import 'package:kanpachi_ui/core/platform/machine_profile.dart';
import 'package:kanpachi_ui/core/platform/single_instance.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/pipe_names.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/tray_bridge.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/window_size_memory.dart';
import 'package:kanpachi_ui/features/update/presentation/cubit/update_cubit.dart';
import 'package:kanpachi_ui/ioc/injector.dart';
import 'package:kanpachi_ui/ioc/ioc_manager.dart';
import 'package:marionette_flutter/marionette_flutter.dart';
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

/// La bandera con la que el daemon dice dónde dejar el registro.
///
/// Es un CONTRATO con `daemon/adapter/uihost`, que la escribe en `launch`. Se
/// llama igual que la del daemon y significa lo mismo: la carpeta, no el
/// archivo. Ver [AppLog.open] para por qué se recibe en vez de deducirse.
const String kLogFlag = '--log';

/// La bandera con la que el daemon dice que está REABRIENDO la sala del
/// arranque anterior, ahora mismo.
///
/// # Qué hueco cierra
///
/// El daemon lo sabe antes de lanzar esta ventana, porque lo lee del disco.
/// Esta ventana no sabía nada hasta conectar y recibir el primer estado, y
/// reabrir tarda: dos redes que levantar, direcciones que tomar, un MTU que
/// medir. Durante todo ese rato se enseñaba la PORTADA, que dice exactamente lo
/// contrario de lo que está pasando, y después se saltaba a la sala de golpe.
///
/// Con la bandera, el primer fotograma ya es la espera correcta, y el latido la
/// rellena con los pasos de verdad en cuanto conecta. **No trae ningún dato de
/// la sala**: dice QUE hay una en camino y nada más. Lo que se enseña sale del
/// daemon, igual que siempre.
///
/// Es un CONTRATO con `daemon/cmd/kanpachid`, que la escribe en su constante
/// `uiResumeFlag`, y falla hacia el lado callado por lo mismo que [kShowFlag]:
/// sin ella la ventana arranca como arrancaba.
const String kResumeHostedRoomFlag = '--resume-hosted-room';

/// La bandera con la que el daemon dice DÓNDE ESCRIBE, o sea dónde está el
/// token.
///
/// Es un CONTRATO con `daemon/adapter/uihost`, que la escribe en `launch`, y se
/// llama igual que la bandera del propio daemon porque significa lo mismo.
///
/// # Qué hueco cierra
///
/// El bundle de un solo ejecutable se descomprime en una carpeta temporal, con
/// el marcador dentro, y arranca al daemon diciéndole que escriba junto al
/// `.exe` que alguien abrió. Esta ventana deducía del marcador que tenía al
/// lado, o sea del temporal, y ahí no hay token: enseñaba que no hay servicio
/// con el servicio corriendo.
///
/// Ver [PipeNames.useDataDir] para por qué esto no rompe la doctrina del
/// marcador. Sin la bandera se deduce como siempre, que es lo correcto en el
/// producto instalado y en la carpeta portable.
const String kDataFlag = '--data';

void main(List<String> args) {
  // **Todo dentro de la zona, empezando por `ensureInitialized`.**
  //
  // No es orden decorativo: Flutter exige que el binding se inicialice en la
  // MISMA zona en la que después corre `runApp`, y avisa con un "Zone mismatch"
  // si no. Inicializarlo fuera y entrar acá solo para `runApp` es el error que
  // parece natural.
  runZonedGuarded<Future<void>>(
    () async {
      // **Lo PRIMERO de todo, antes de que nadie pregunte por la carpeta de
      // datos.** El registro ya la usa de respaldo tres líneas más abajo.
      PipeNames.useDataDir(_valorDe(args, kDataFlag) ?? '');

      AppLog.open(dir: _valorDe(args, kLogFlag));

      // **El arranque también se anota, no solo los errores.** Un registro que
      // solo tiene fallos no distingue "se cerró sola" de "no llegó a
      // arrancar", que es exactamente la pregunta que hay abierta sobre esta
      // ventana. Y es la primera línea que se escribe, así que también prueba
      // que el archivo se puede escribir.
      AppLog.info(
        'la interfaz arrancó',
        'pid $pid portable ${PipeNames.isPortable} '
            'argumentos ${args.join(' ')}',
      );

      // Los errores del framework: los que Flutter atrapa al construir, medir o
      // pintar. No pasan por la zona, así que sin esto no quedan en ningún
      // sitio con un binario gráfico.
      FlutterError.onError = (FlutterErrorDetails details) {
        AppLog.error('framework', details.exception, details.stack);
        FlutterError.presentError(details);
      };

      // Y lo que se le escapa a la zona, que es lo que llega desde el motor:
      // callbacks de la plataforma y errores de un `Future` sin dueño.
      // Contestar `true` significa "ya lo conté", y aun así se sigue.
      PlatformDispatcher.instance.onError = (Object error, StackTrace stack) {
        AppLog.error('plataforma', error, stack);
        return true;
      };

      await _arrancar(args);
    },
    (Object error, StackTrace stack) {
      AppLog.error('zona', error, stack);
    },
  );
}

/// Lo que el daemon puso detrás de una bandera, o null si no puso nada.
///
/// Formato `<bandera> <valor>`, con el valor en el argumento siguiente. Sin
/// valor detrás, la bandera se ignora: es lo que pasa si alguien la escribe a
/// mano, y no vale la pena tumbar la ventana por eso.
String? _valorDe(List<String> args, String bandera) {
  final int i = args.indexOf(bandera);
  if (i < 0 || i + 1 >= args.length) return null;
  final String valor = args[i + 1].trim();
  return valor.isEmpty ? null : valor;
}

Future<void> _arrancar(List<String> args) async {
  // In debug, Marionette's binding takes the regular one's place so an MCP
  // agent can drive this window: tap, type, screenshot, hot reload. It extends
  // WidgetsFlutterBinding, so everything downstream sees the binding it
  // expects. A release build boots the plain binding and never loads it.
  if (kDebugMode) {
    MarionetteBinding.ensureInitialized();
  } else {
    WidgetsFlutterBinding.ensureInitialized();
  }

  // **Antes que nada.** Si ya hay un Kanpachi corriendo, este proceso solo
  // existe para avisarle, y eso ya lo hizo `claim`. Seguir adelante abriría una
  // segunda ventana y un segundo icono en la bandeja.
  //
  // Pasa de verdad y no es un caso raro: es cómo el daemon abre la ventana
  // cuando alguien hace doble clic en el acceso directo con Kanpachi ya
  // corriendo, y es cómo va a entrar un enlace `kanpachi://`.
  final bool onlyOne = SingleInstance.claim(
    eventName: PipeNames.defaultInstanceName,
    onShowRequested: _traerAlFrente,
  );
  if (!onlyOne) {
    // Se anota porque si no, este cierre se lee como una muerte: el registro
    // tendría un arranque sin nada detrás, que es la forma de un fallo.
    AppLog.info('ya había otra ventana, esta le avisó y se va');
    exit(0);
  }

  // What this window remembers between runs, read BEFORE the first frame.
  //
  // It has to be read here and not later: it decides which screen opens. Read
  // after the first frame, onboarding would flash and then disappear, which is
  // worse than showing it.
  //
  // A portable copy narrates by default. See [AppPreferences.verbose] for why,
  // and note this is the marker on disk answering, not a build flag: one
  // `kanpachiui.exe` serves both the installed product and the portable bundle.
  // Donde escribe el daemon, que en una copia portable es la carpeta desde
  // donde la abrieron. Antes iban a `%APPDATA%`, o sea fuera de la carpeta y
  // compartidos con cualquier otro Kanpachi de esta máquina.
  final AppPreferences preferences = await AppPreferences.open(
    dir: PipeNames.dataDir,
    defaultVerbose: kDebugMode || PipeNames.isPortable,
  );

  // El nombre ya no está ahí dentro: lo guarda el daemon, y esta ventana lo LEE
  // del mismo directorio, igual que lee el token. Sigue siendo una lectura de
  // disco antes del primer frame, así que la decisión de qué pantalla abrir no
  // pasa a depender de que el daemon ya esté arriba. Ver [MachineProfile].
  final MachineProfile profile = await MachineProfile.open(
    dir: PipeNames.dataDir,
  );

  // El marcador se lee ACÁ, una vez, y baja por el registro de dependencias.
  // La pantalla que lo necesita vive en presentación y no puede importar infra.
  IocManager.register(
    preferences: preferences,
    profile: profile,
    portable: PipeNames.isPortable,
    resumingHostedRoom: args.contains(kResumeHostedRoomFlag),
  );
  await _prepareWindow(
    silent: await _shouldStayQuiet(args),
    size: preferences.windowSize ?? AppSpacing.initialWindow,
  );
  runApp(KanpachiApp(onboarded: profile.onboarded, preferences: preferences));
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
        BlocProvider<UpdateCubit>(
          // **Sin `..check()`.** Arrancar ya no pregunta nada: la comprobación
          // pasó a ser a pedido, desde los ajustes. Ver [UpdateCubit].
          //
          // Lo que sí hace el cubit al nacer es leer lo que ya se sabía, que
          // está en disco, así que el aviso reaparece sin salir a la red.
          create: (_) => Injector.instance.get<UpdateCubit>(),
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
      scrollBehavior: const _SinBarra(),
      home: const ShellPage(),
    );
  }
}

/// El comportamiento de scroll de la app: sin barra, y con arrastre.
///
/// Va acá y no quitando widgets porque la barra no la pone nadie a mano. En
/// escritorio, `MaterialScrollBehavior.buildScrollbar` le envuelve una a TODO
/// scroll vertical, así que borrar un `Scrollbar` de una pantalla no la quita de
/// las demás y la siguiente lista que alguien escriba la trae otra vez.
///
/// `dragDevices` gana el ratón, que es lo que en escritorio Flutter no incluye
/// por defecto. Sin eso, quitar la barra dejaría contenido al que solo se llega
/// con la rueda.
class _SinBarra extends MaterialScrollBehavior {
  const _SinBarra();

  @override
  Widget buildScrollbar(
    BuildContext context,
    Widget child,
    ScrollableDetails details,
  ) => child;

  @override
  Set<PointerDeviceKind> get dragDevices => <PointerDeviceKind>{
    PointerDeviceKind.touch,
    PointerDeviceKind.mouse,
    PointerDeviceKind.trackpad,
    PointerDeviceKind.stylus,
  };
}
