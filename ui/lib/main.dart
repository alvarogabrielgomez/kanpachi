import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';
import 'package:kanpachi_ui/ioc/injector.dart';
import 'package:kanpachi_ui/ioc/ioc_manager.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  IocManager.register();
  runApp(const KanpachiApp());
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
