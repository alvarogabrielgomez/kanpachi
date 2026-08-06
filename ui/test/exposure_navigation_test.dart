import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/infra/fake_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';

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
  late Room sala;

  setUpAll(() async {
    sala = await FakeSessionRepository().createRoom(
      name: 'La Guarida',
      nickname: 'Alvaro',
    );
  });

  /// Monta el armazón ENTERO, y ahí está la gracia: es lo único que puede
  /// afirmar que una pantalla es alcanzable, porque es lo que enruta.
  Future<(ShellCubit, SessionCubit)> pinta(WidgetTester tester) async {
    final ShellCubit shell = ShellCubit(initial: AppScreen.room);
    final SessionCubit session = SessionCubit(FakeSessionRepository())
      ..debugReplaceRoom(sala);

    await tester.pumpWidget(
      MultiBlocProvider(
        providers: <BlocProvider<dynamic>>[
          BlocProvider<ShellCubit>.value(value: shell),
          BlocProvider<SessionCubit>.value(value: session),
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

    await tester.tap(find.text(enlace));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

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

    await tester.tap(find.text(enlace));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    await tester.tap(find.byType(AppBackButton));
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));

    expect(shell.state.screen, equals(AppScreen.room));
    expect(find.text(tituloExposicion), findsNothing);
    expect(find.text(enlace), findsOneWidget);
  });
}
