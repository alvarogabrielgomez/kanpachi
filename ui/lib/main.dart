import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';
import 'package:kanpachi_ui/ioc/injector.dart';
import 'package:kanpachi_ui/ioc/ioc_manager.dart';
import 'package:window_manager/window_manager.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await _prepareWindow();
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
Future<void> _prepareWindow() async {
  await windowManager.ensureInitialized();
  const WindowOptions options = WindowOptions(
    size: AppSpacing.initialWindow,
    minimumSize: AppSpacing.minWindow,
    center: true,
    backgroundColor: Colors.transparent,
    skipTaskbar: false,
    titleBarStyle: TitleBarStyle.hidden,
    windowButtonVisibility: false,
    title: 'Kanpachi',
  );
  await windowManager.waitUntilReadyToShow(options, () async {
    await windowManager.show();
    await windowManager.focus();
  });
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
          create: (_) => Injector.instance.get<SessionCubit>()..loadCatalog(),
        ),
      ],
      child: const _ThemedApp(),
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
