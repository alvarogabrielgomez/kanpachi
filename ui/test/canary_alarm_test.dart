import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/canary_alarm.dart';
import 'package:kanpachi_ui/features/session/domain/entities/canary.dart';

/// La alarma de la Protección Kanpachi.
///
/// Lo que se afirma acá no es cómo se ve: es CUÁNDO aparece y cuándo no, que es
/// donde el diseño se puede romper en silencio. El daemon repara la primera fuga
/// solo y sin avisar, así que un banner que fuera por el veredicto delataría un
/// problema ya arreglado, y uno que se apagara con una ronda sin confirmar
/// borraría una alarma que sigue siendo cierta.
void main() {
  const String tituloAlarma = 'Tu protección no está conteniendo';

  Future<int> pinta(
    WidgetTester tester, {
    required List<AlertKind> alerts,
    CanaryCheck check = const CanaryCheck.blind(),
    bool busy = false,
  }) async {
    int pulsaciones = 0;
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: SingleChildScrollView(
            child: CanaryAlarm(
              alerts: alerts,
              check: check,
              busy: busy,
              onReapply: () async => pulsaciones++,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    return pulsaciones;
  }

  CanaryCheck fuga({int port = 51234}) => CanaryCheck(
    measured: true,
    verdict: CanaryVerdict.leaking,
    port: port,
    touched: true,
    measuredAt: DateTime(2026, 8, 4, 22, 15, 3),
    asked: const <String>['humberto', 'marisol'],
  );

  testWidgets('con la alerta del canario se enseña la alarma', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      alerts: <AlertKind>[AlertKind.gateLeaking],
      check: fuga(),
    );

    expect(find.text(tituloAlarma), findsOneWidget);
    expect(find.text('Volver a aplicar la protección'), findsOneWidget);
  });

  testWidgets('sin la alerta no se enseña nada', (WidgetTester tester) async {
    await pinta(
      tester,
      alerts: <AlertKind>[AlertKind.firewallOff],
      check: fuga(),
    );

    expect(find.text(tituloAlarma), findsNothing);
  });

  // EL CASO QUE JUSTIFICA IR POR LA ALERTA Y NO POR EL VEREDICTO.
  //
  // Una fuga que el daemon ya reparó sola tiene veredicto `leaking` y NO tiene
  // alerta. Pintarla sería enterar al usuario de un problema arreglado, que es
  // como se le enseña a ignorar los avisos.
  testWidgets('una fuga ya reparada sola no pinta nada', (
    WidgetTester tester,
  ) async {
    await pinta(tester, alerts: const <AlertKind>[], check: fuga());

    expect(find.text(tituloAlarma), findsNothing);
  });

  // EL OTRO LADO DEL MISMO CASO.
  //
  // Una ronda que nadie contestó llega con veredicto `unconfirmed` y con la
  // alarma todavía puesta. Yendo por el veredicto, el banner desaparecería y la
  // pantalla diría que se arregló algo que sigue roto.
  testWidgets('una ronda sin confirmar no borra el banner', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      alerts: <AlertKind>[AlertKind.gateLeaking],
      check: const CanaryCheck(
        measured: true,
        verdict: CanaryVerdict.unconfirmed,
      ),
    );

    expect(find.text(tituloAlarma), findsOneWidget);
  });

  // EL PUERTO NO SE NOMBRA, Y ES UNA REGLA ESCRITA EN docs/05-ui.md.
  //
  // El canario vive en un puerto al azar que Kanpachi abrió hace dos segundos y
  // YA CERRÓ. Nombrarlo manda al usuario a buscar algo que no existe, y de paso
  // dice que lo que falló es un hueco cuando lo que falló es la contención
  // entera. Lo que sí va es quién lo comprobó: es lo que hace creíble la frase y
  // explica por qué el aviso aparece ahora y no antes.
  testWidgets('el detalle dice quién lo comprobó y jamás el puerto', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      alerts: <AlertKind>[AlertKind.gateLeaking],
      check: fuga(port: 49876),
    );

    expect(
      find.textContaining('49876'),
      findsNothing,
      reason:
          'ese puerto ya está cerrado: nombrarlo manda al usuario a buscar algo que no existe',
    );
    expect(find.textContaining('2 personas de la sala'), findsOneWidget);
  });

  testWidgets('con un solo preguntado se le nombra', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      alerts: <AlertKind>[AlertKind.gateLeaking],
      check: const CanaryCheck(
        measured: true,
        verdict: CanaryVerdict.leaking,
        touched: true,
        asked: <String>['humberto'],
      ),
    );

    expect(find.textContaining('desde la PC de humberto'), findsOneWidget);
  });

  testWidgets('una comprobación ciega enseña la alarma sin inventar detalle', (
    WidgetTester tester,
  ) async {
    await pinta(tester, alerts: <AlertKind>[AlertKind.gateLeaking]);

    expect(find.text(tituloAlarma), findsOneWidget);
    expect(find.textContaining('Comprobado'), findsNothing);
  });

  testWidgets('pulsar repone una sola vez', (WidgetTester tester) async {
    int pulsaciones = 0;
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: CanaryAlarm(
            alerts: const <AlertKind>[AlertKind.gateLeaking],
            check: fuga(),
            onReapply: () async => pulsaciones++,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('Volver a aplicar la protección'));
    await tester.pumpAndSettle();

    expect(pulsaciones, equals(1));
  });

  // El doble clic sobre un botón que ESCRIBE EN EL FIREWALL manda dos
  // escrituras a la vez. Deshabilitarlo mientras trabaja no es cosmético.
  testWidgets('mientras repone el botón no se puede volver a pulsar', (
    WidgetTester tester,
  ) async {
    int pulsaciones = 0;
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: CanaryAlarm(
            alerts: const <AlertKind>[AlertKind.gateLeaking],
            check: fuga(),
            busy: true,
            onReapply: () async => pulsaciones++,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('Reponiendo…'), warnIfMissed: false);
    await tester.pumpAndSettle();

    expect(pulsaciones, equals(0));
  });
}
