import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/platform/single_instance.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/tray_bridge.dart';
import 'package:kanpachi_ui/ioc/injector.dart';
import 'package:kanpachi_ui/ioc/ioc_manager.dart';
import 'package:window_manager/window_manager.dart';

/// La bandera con la que el daemon pide arrancar sin abrir ventana.
///
/// Es un CONTRATO con `daemon/cmd/kanpachid`, que la escribe en su propia
/// constante `uiSilentFlag`. De los que no dan error al romperse: cambiar una
/// sin la otra produce una ventana que se abre al encender la PC, o que no se
/// abre nunca.
const String kSilentFlag = '--silent';

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

  await _prepareWindow(silent: args.contains(kSilentFlag));
  IocManager.register();
  runApp(const KanpachiApp());
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
Future<void> _prepareWindow({required bool silent}) async {
  await windowManager.ensureInitialized();
  final WindowOptions options = WindowOptions(
    size: AppSpacing.initialWindow,
    minimumSize: AppSpacing.minWindow,
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
  // Medido: con `--silent` y solo omitiendo `show()`, `IsWindowVisible`
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
  const KanpachiApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MultiBlocProvider(
      providers: <BlocProvider<dynamic>>[
        BlocProvider<ShellCubit>(
          create: (_) => Injector.instance.get<ShellCubit>(),
        ),
        BlocProvider<SessionCubit>(
          // La salud se pide en el arranque y no cuando el usuario abre una
          // sala: los avisos que importan en la portada, con el Firewall de
          // Windows apagado a la cabeza, son ciertos antes de que haya nada que
          // hospedar, y esperar a la sala sería avisar tarde.
          create: (_) => Injector.instance.get<SessionCubit>()
            ..loadCatalog()
            ..refreshHealth(),
        ),
      ],
      // Por encima de la app y por debajo de los cubits: tiene que durar lo
      // que dure la ventana y necesita leer la sala para escribir el menú de
      // la bandeja.
      child: const TrayBridge(child: _ThemedApp()),
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
