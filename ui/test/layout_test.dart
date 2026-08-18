import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_methods.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/pages/shell_page.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/shell_bars.dart';
import 'package:kanpachi_ui/features/update/domain/repositories/update_repository.dart';
import 'package:kanpachi_ui/features/update/presentation/cubit/update_cubit.dart';

import 'daemon_client_test.dart' show daemonTestConnector;

/// Contesta que no hay versión nueva, sin salir a la red. Ver el mismo doble en
/// `exposure_navigation_test.dart`.
class _SinVersionNueva implements UpdateRepository {
  const _SinVersionNueva();

  @override
  Future<String?> latestVersion() async => null;
}

/// El candado del layout: ninguna pantalla se desborda en ningún tamaño de
/// ventana que la app permita.
///
/// Esto no es una app de móvil con un ancho que decide el sistema: la ventana
/// la estira el usuario, desde el mínimo que impone `main()` hasta la pantalla
/// entera, y lo hace mientras la app corre. Un `Row` que cabe en la ventana con
/// la que abre y revienta 80 px más estrecho es un fallo que sólo aparece
/// cuando alguien arrastra un borde — o sea, en su PC y no en la nuestra.
///
/// Flutter reporta los desbordes al pintar, así que basta con pintar cada
/// pantalla en cada tamaño y escuchar. Es feo de ver y barato de correr.
void main() {
  /// Los tres tamaños que importan, y por qué cada uno.
  ///
  /// El mínimo y el inicial salen de los mismos tokens que usa `main()`: si
  /// alguien baja el mínimo, este test empieza a probar el nuevo mínimo solo.
  const Map<String, Size> ventanas = <String, Size>{
    'la ventana mínima': AppSpacing.minWindow,
    'la ventana del diseño': AppSpacing.initialWindow,
    'maximizada en 1440p': Size(2560, 1440),
  };

  const Map<String, Object?> salaDeHost = <String, Object?>{
    'conn': 'connected',
    'role': 'host',
    'name': 'La Guarida de los Panas',
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
      <String, Object?>{
        'ip': '100.87.3.2',
        'name': 'Humberto',
        'path': 'relay',
        'rtt_ms': 45,
      },
    ],
  };

  const Map<String, Object?> invitacion = <String, Object?>{
    'link': 'kanpachi://A7K2-M9QX',
    'code': 'A7K2-M9QX',
    'seed': 'kanpachi.accentio.dev',
    'room': 'La Guarida de los Panas',
    'host': 'Alvaro',
  };

  /// Pinta el marco entero con la pantalla puesta y devuelve los desbordes.
  ///
  /// Devuelve la lista en vez de hacer `expect` adentro para que el mensaje de
  /// fallo pueda decir en qué tamaño y en qué pantalla pasó, que es lo único
  /// que hace falta saber para arreglarlo.
  Future<List<String>> desbordesDe(
    WidgetTester tester, {
    required Size ventana,
    required AppScreen pantalla,
    AppDialog dialogo = AppDialog.none,

    /// Con qué petición se abre [AppDialog.trustSeed]. Sin ella ese diálogo se
    /// dibuja vacío, y el test pasaría midiendo nada.
    TrustRequest? confianza,
    Map<String, Object?>? sala,
    Map<String, Object?>? invite,
    ThemeMode tema = ThemeMode.dark,
  }) async {
    final List<String> desbordes = <String>[];
    final FlutterExceptionHandler? anterior = FlutterError.onError;
    FlutterError.onError = (FlutterErrorDetails detalles) {
      final String texto = detalles.exceptionAsString();
      // Sólo los desbordes son asunto de este test. Cualquier otro error se
      // devuelve al manejador del framework para que rompa la suite como
      // siempre: tragarlos acá escondería fallos que no tienen nada que ver.
      if (texto.contains('overflowed')) {
        desbordes.add(texto.split('\n').first);
      } else {
        anterior?.call(detalles);
      }
    };

    final ShellCubit shell = ShellCubit(initial: pantalla);
    Object? responde(String method, Map<String, Object?>? _) =>
        switch (method) {
          DaemonMethods.status => sala ?? <String, Object?>{},
          DaemonMethods.pendingInvite => invite ?? <String, Object?>{},
          DaemonMethods.savedRoom => <String, Object?>{'found': false},
          DaemonMethods.listGames || DaemonMethods.foreignRules => <Object?>[],
          _ => <String, Object?>{},
        };
    final SessionCubit session = SessionCubit(
      PipeSessionRepository(daemonTestConnector(responde)),
    )..watchSession();
    if (confianza != null) {
      shell.askTrust(confianza);
    } else if (dialogo != AppDialog.none) {
      shell.showDialog(dialogo);
    }

    try {
      await tester.binding.setSurfaceSize(ventana);
      await tester.pumpWidget(
        _Sujeto(shell: shell, session: session, tema: tema),
      );
      // Sin `pumpAndSettle`: el fondo ambiental y el punto de estado laten para
      // siempre, así que nunca se asienta. Dos frames y el tiempo de la
      // animación de entrada alcanzan para pintar todo ya colocado.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      // Guarda contra el verde falso. Con el cuerpo colapsado a cero de ancho
      // casi nada se desborda — un `Column` sólo reporta el desborde de su eje
      // principal — así que "no hubo desbordes" no querría decir nada. Si la
      // pantalla trae contenido, tiene que medir de verdad antes de creerle al
      // resto del test.
      final Finder cuerpo = find.byType(ScreenEnter);
      if (cuerpo.evaluate().isNotEmpty) {
        expect(
          tester.getSize(cuerpo.first).width,
          greaterThan(400),
          reason:
              '$pantalla se dibujó en un cuerpo colapsado: el test no '
              'estaría probando nada.',
        );
      }

      // **Un diálogo que no entra NO desborda, y ese fue el verde falso.**
      // [AppModal] mete su contenido en un `SingleChildScrollView`, que se
      // queda tan contento con lo que no cabe: recorta y deja recorrer, sin
      // una línea de aviso. El caso real es del 2026-08-18: el diálogo del
      // alta cortaba su propio botón por abajo con los tres tamaños en verde,
      // y lo vio el dueño en pantalla.
      //
      // Lo que se exige NO es que no haya scroll —una ventana de 520 px de
      // alto con un diálogo largo lo va a necesitar, y para eso está—, es que
      // **el botón que cierra el diálogo se vea sin recorrer nada**. Un
      // diálogo cuyo texto se recorre es incómodo; uno cuya única salida está
      // fuera de la pantalla es un callejón.
      if (dialogo != AppDialog.none || confianza != null) {
        final Finder botones = find.descendant(
          of: find.byType(AppModal),
          matching: find.byType(AppButton),
        );
        for (final Element e in botones.evaluate()) {
          final RenderBox caja = e.renderObject! as RenderBox;
          final double abajo =
              caja.localToGlobal(Offset.zero).dy + caja.size.height;
          if (abajo > ventana.height + 0.5) {
            desbordes.add(
              'el botón "${(e.widget as AppButton).label}" del diálogo '
              'termina en ${abajo.toStringAsFixed(0)} px y la ventana mide '
              '${ventana.height.toInt()}: hay que recorrer para llegar a él.',
            );
          }
        }
      }
    } finally {
      FlutterError.onError = anterior;
      await tester.binding.setSurfaceSize(null);
      await shell.close();
      await session.close();
    }
    return desbordes;
  }

  for (final MapEntry<String, Size> ventana in ventanas.entries) {
    group('En ${ventana.key} (${ventana.value.width.toInt()}×'
        '${ventana.value.height.toInt()})', () {
      for (final AppScreen pantalla in AppScreen.values) {
        testWidgets('$pantalla cabe', (WidgetTester tester) async {
          // Las pantallas de sala necesitan una sala plantada; el resto la
          // ignora. La de invitado trae host ajeno y una regla del firewall,
          // que es el estado con más cosas en pantalla a la vez.
          final Map<String, Object?>? sala = switch (pantalla) {
            AppScreen.room => salaDeHost,
            AppScreen.exposure => salaDeHost,
            _ => null,
          };
          final Map<String, Object?>? invite = pantalla == AppScreen.invite
              ? invitacion
              : null;

          final List<String> desbordes = await desbordesDe(
            tester,
            ventana: ventana.value,
            pantalla: pantalla,
            sala: sala,
            invite: invite,
          );

          expect(
            desbordes,
            isEmpty,
            reason:
                '$pantalla se desborda en ${ventana.key}. Algo tiene ancho '
                'fijo donde tendría que ceder:\n${desbordes.join('\n')}',
          );
        });
      }

      // El de confianza va aparte porque necesita su petición, y con los DOS
      // momentos: el texto y las etiquetas de los botones cambian enteros entre
      // abrir y entrar, así que medir uno solo deja el otro sin medir. El
      // registro va largo a propósito, que es el caso que desbordaría.
      for (final MapEntry<String, TrustRequest> caso in <String, TrustRequest>{
        'al abrir': const TrustRequest.hosting(
          seed: 'registro.muy-largo.ejemplo.com',
          suggestedName: 'Sala de Kanpachi',
        ),
        'al entrar': const TrustRequest.joining(
          seed: 'registro.muy-largo.ejemplo.com',
          code: 'A7K2M9QX@registro.muy-largo.ejemplo.com',
        ),
      }.entries) {
        testWidgets('AppDialog.trustSeed ${caso.key} cabe', (
          WidgetTester tester,
        ) async {
          final List<String> desbordes = await desbordesDe(
            tester,
            ventana: ventana.value,
            pantalla: AppScreen.home,
            confianza: caso.value,
          );

          expect(desbordes, isEmpty, reason: desbordes.join('\n'));
        });
      }

      for (final AppDialog dialogo in <AppDialog>[
        AppDialog.confirmKick,
        AppDialog.confirmRenew,
      ]) {
        testWidgets('$dialogo cabe', (WidgetTester tester) async {
          final List<String> desbordes = await desbordesDe(
            tester,
            ventana: ventana.value,
            pantalla: AppScreen.room,
            dialogo: dialogo,
            sala: salaDeHost,
          );

          expect(desbordes, isEmpty, reason: desbordes.join('\n'));
        });
      }

      // El del alta va sobre SU pantalla y sin sala, que es donde se abre de
      // verdad: es el más largo de todos, con tres bloques y una despedida.
      testWidgets('${AppDialog.recommendedSetup} cabe', (
        WidgetTester tester,
      ) async {
        final List<String> desbordes = await desbordesDe(
          tester,
          ventana: ventana.value,
          pantalla: AppScreen.nickname,
          dialogo: AppDialog.recommendedSetup,
        );

        expect(desbordes, isEmpty, reason: desbordes.join('\n'));
      });
    });
  }

  testWidgets('el contenido deja de crecer en el tope y se centra', (
    WidgetTester tester,
  ) async {
    // La otra mitad del trabajo: que no se desborde es lo mínimo, pero una
    // ventana maximizada estirando dos columnas a 1200 px de ancho tampoco
    // sirve. Se mide la barra de estado, que sí ocupa la ventana entera, contra
    // el contenido, que se planta en el tope.
    await tester.binding.setSurfaceSize(const Size(2560, 1440));
    addTearDown(() => tester.binding.setSurfaceSize(null));

    final ShellCubit shell = ShellCubit(initial: AppScreen.home);
    final SessionCubit session = SessionCubit(
      PipeSessionRepository(
        daemonTestConnector(
          (String method, Map<String, Object?>? _) => switch (method) {
            DaemonMethods.status ||
            DaemonMethods.pendingInvite => <String, Object?>{},
            DaemonMethods.savedRoom => <String, Object?>{'found': false},
            DaemonMethods.listGames => <Object?>[],
            _ => <String, Object?>{},
          },
        ),
      ),
    );
    addTearDown(shell.close);
    addTearDown(session.close);

    await tester.pumpWidget(
      _Sujeto(shell: shell, session: session, tema: ThemeMode.dark),
    );
    await tester.pump(const Duration(milliseconds: 400));

    // La barra de estado sí ocupa la ventana entera: es el marco, y el marco
    // no se centra. Sirve de referencia para comprobar que el tope de abajo es
    // del contenido y no de que la ventana sea chica.
    expect(tester.getSize(find.byType(ShellStatusBar)).width, 2560);

    final Finder tope = find.byWidgetPredicate(
      (Widget w) =>
          w is ConstrainedBox &&
          w.constraints.maxWidth == AppSpacing.contentMax,
    );
    expect(tope, findsOneWidget);
    expect(tester.getSize(tope).width, AppSpacing.contentMax);

    // Centrado: lo que sobra queda repartido a los dos lados, no todo a uno.
    final double izquierda = tester.getTopLeft(tope).dx;
    final double derecha = 2560 - tester.getTopRight(tope).dx;
    expect(izquierda, closeTo(derecha, 1));
  });
}

/// La app entera con dos cubits puestos a mano.
///
/// No pasa por el `IocManager` a propósito: sus cubits son singletons, y un
/// test que reutiliza el estado del anterior falla o pasa según el orden en que
/// se corran, que es la peor clase de test que hay.
class _Sujeto extends StatelessWidget {
  const _Sujeto({
    required this.shell,
    required this.session,
    required this.tema,
  });

  final ShellCubit shell;
  final SessionCubit session;
  final ThemeMode tema;

  @override
  Widget build(BuildContext context) {
    return MultiBlocProvider(
      providers: <BlocProvider<dynamic>>[
        BlocProvider<ShellCubit>.value(value: shell),
        BlocProvider<SessionCubit>.value(value: session),
        BlocProvider<UpdateCubit>(
          create: (_) => UpdateCubit(const _SinVersionNueva()),
        ),
      ],
      child: MaterialApp(
        debugShowCheckedModeBanner: false,
        themeMode: tema,
        theme: AppTheme.light(density: DensityTokens.balanced),
        darkTheme: AppTheme.dark(density: DensityTokens.balanced),
        home: const ShellPage(),
      ),
    );
  }
}
