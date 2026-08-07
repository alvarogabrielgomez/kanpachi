import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_methods.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';
import 'package:kanpachi_ui/features/update/domain/repositories/update_repository.dart';
import 'package:kanpachi_ui/features/update/presentation/cubit/update_cubit.dart';

import 'daemon_client_test.dart' show daemonTestConnector;

/// El marco pregunta por versiones nuevas al abrirse y cerrarse una sala, así
/// que montarlo entero necesita a quién preguntarle. Éste contesta que no hay,
/// sin red: un test que sale a internet falla el día que el CI no tiene salida.
class _SinVersionNueva implements UpdateRepository {
  const _SinVersionNueva();

  @override
  Future<String?> latestVersion() async => null;
}

/// El camino hasta la medición, y la vuelta.
///
/// # Qué se afirma acá, y por qué hace falta afirmarlo
///
/// `ExposurePage` existe desde hace tiempo con su propio test, y **no la montaba
/// ninguna pantalla de la app**: solo la pintaba ese test. O sea que estaba
/// escrita, probada, y era inalcanzable para el usuario, que es la peor forma de
/// que algo exista. Nada lo decía, porque un test de widget pinta lo que le
/// pasen y no pregunta si alguien más lo pinta.
///
/// Estas afirmaciones son el enlace que faltaba: que se llega desde la sala, y
/// que se vuelve. Sin la vuelta, la pantalla es una trampa: el usuario entra a
/// mirar sus puertos y se queda sin su sala.
void main() {
  const Map<String, Object?> sala = <String, Object?>{
    'conn': 'connected',
    'role': 'host',
    'name': 'La Guarida',
    'code': 'A7K2-M9QX',
    'link': 'https://kanpachi.accentio.dev/A7K2-M9QX#test',
    'host_present': true,
    'peers': <Object?>[
      <String, Object?>{
        'ip': '100.87.3.1',
        'name': 'Alvaro',
        'path': 'self',
        'self': true,
        'host': true,
      },
    ],
  };

  Object? responde(String method, Map<String, Object?>? _) => switch (method) {
    DaemonMethods.status => sala,
    DaemonMethods.listGames || DaemonMethods.foreignRules => <Object?>[],
    _ => <String, Object?>{},
  };

  /// Monta el armazón ENTERO, y ahí está la gracia: es lo único que puede
  /// afirmar que una pantalla es alcanzable, porque es lo que enruta.
  Future<(ShellCubit, SessionCubit)> pinta(WidgetTester tester) async {
    await tester.binding.setSurfaceSize(AppSpacing.initialWindow);
    addTearDown(() => tester.binding.setSurfaceSize(null));
    final ShellCubit shell = ShellCubit(initial: AppScreen.room);
    final SessionCubit session = SessionCubit(
      PipeSessionRepository(daemonTestConnector(responde)),
    );
    await session.refresh();

    await tester.pumpWidget(
      MultiBlocProvider(
        providers: <BlocProvider<dynamic>>[
          BlocProvider<ShellCubit>.value(value: shell),
          BlocProvider<SessionCubit>.value(value: session),
          BlocProvider<UpdateCubit>(
            create: (_) => UpdateCubit(const _SinVersionNueva()),
          ),
        ],
        child: MaterialApp(
          debugShowCheckedModeBanner: false,
          theme: AppTheme.light(density: DensityTokens.balanced),
          home: const ShellPage(),
        ),
      ),
    );
    // Sin pumpAndSettle: el fondo ambiental late para siempre y nunca se
    // asienta. Dos frames y la animación de entrada alcanzan.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    return (shell, session);
  }

  const String tituloExposicion = 'Qué tiene abierto tu PC';
  const String enlace = 'Ver lo que tu PC tiene abierto';

  Finder botonExposicion() => find.byWidgetPredicate(
    (Widget widget) => widget is AppButton && widget.label == enlace,
  );

  Future<void> abreExposicion(WidgetTester tester) async {
    final Finder boton = botonExposicion();
    await tester.ensureVisible(boton);
    await tester.pump();
    await tester.tap(boton);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
  }

  testWidgets('desde la sala se llega a la medición', (
    WidgetTester tester,
  ) async {
    final (ShellCubit shell, SessionCubit session) = await pinta(tester);
    addTearDown(() async {
      await shell.close();
      await session.close();
    });

    expect(
      find.text(enlace),
      findsOneWidget,
      reason: 'sin este enlace la pantalla de exposición no la alcanza nadie',
    );

    await abreExposicion(tester);

    expect(find.text(tituloExposicion), findsOneWidget);
    expect(shell.state.screen, equals(AppScreen.exposure));
  });

  // SIN LA VUELTA, LA PANTALLA ES UNA TRAMPA.
  //
  // El usuario entra a mirar sus puertos y se queda sin su sala, sin nada en
  // pantalla que lo saque de ahí. Es el fallo que más fácil se cuela al montar
  // una pantalla nueva, porque ir funciona a la primera y volver hay que
  // acordarse de escribirlo.
  testWidgets('y se vuelve a la sala', (WidgetTester tester) async {
    final (ShellCubit shell, SessionCubit session) = await pinta(tester);
    addTearDown(() async {
      await shell.close();
      await session.close();
    });

    await abreExposicion(tester);

    await tester.tap(find.byType(AppBackButton));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    expect(shell.state.screen, equals(AppScreen.room));
    expect(find.text(tituloExposicion), findsNothing);
    expect(find.text(enlace), findsOneWidget);
  });
}
