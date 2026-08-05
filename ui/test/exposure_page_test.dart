import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/exposure_page.dart';
import 'package:kanpachi_ui/features/session/domain/entities/exposure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';

/// La pantalla que contesta "¿qué tiene abierto mi PC?".
///
/// Lo que se afirma acá no es cómo se ve: es que **no puede mentir**. Una
/// medición que falló no se puede pintar como una lista buena, y una lista de
/// puertos no se puede enseñar sin decir qué pasa con todo lo demás.
void main() {
  Future<void> pinta(WidgetTester tester, ExposureReport report) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: SingleChildScrollView(
            child: ExposurePage(
              load: () async => report,
              // El sondeo es de otra sección y tiene su propio test. Acá va la
              // cara del host, que es la que no marca nada.
              probe: () async => const ProbeReport.blind(),
              isHost: true,
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  const ExposedPort juego = ExposedPort(
    proto: 'udp',
    from: 16261,
    to: 16262,
    applied: true,
    isControl: false,
    reachableBy: <String>['100.64.1.5'],
  );

  testWidgets('una medición que falló no enseña ninguna lista', (
    WidgetTester tester,
  ) async {
    await pinta(tester, const ExposureReport.blind());

    expect(
      find.textContaining('no pudo leer lo que tiene puesto'),
      findsOneWidget,
      reason: 'la pantalla ciega tiene que decir que nadie está mirando',
    );
    expect(
      find.textContaining('16261'),
      findsNothing,
      reason:
          'un informe ciego no puede pintar puertos: sería la última lista '
          'buena sobre una medición que no ocurrió',
    );
  });

  testWidgets(
    'la lista nunca va sola: la segunda fila dice qué pasa con el resto',
    (WidgetTester tester) async {
      await pinta(
        tester,
        ExposureReport(
          measured: true,
          measuredAt: DateTime(2026, 8, 4, 20, 14, 3),
          gate: GateState.present,
          ports: const <ExposedPort>[juego],
        ),
      );

      expect(find.textContaining('UDP 16261-16262'), findsOneWidget);
      expect(find.textContaining('100.64.1.5'), findsOneWidget);
      // Sin esta fila la lista es cierta y engañosa a la vez: enumera lo propio
      // sin decir nada de la puerta de al lado.
      expect(find.textContaining('Todo lo demás está cerrado'), findsOneWidget);
      expect(find.textContaining('20:14:03'), findsOneWidget);
    },
  );

  testWidgets('sin compuerta la pantalla lo dice, y no calla', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      ExposureReport(
        measured: true,
        measuredAt: DateTime(2026, 8, 4, 20, 14, 3),
        gate: GateState.absent,
        ports: const <ExposedPort>[juego],
      ),
    );

    expect(find.textContaining('no está cerrado el resto'), findsOneWidget);
    expect(
      find.textContaining('Todo lo demás está cerrado'),
      findsNothing,
      reason: 'con la compuerta ausente no se puede afirmar lo contrario',
    );
  });

  testWidgets('un puerto que se pidió y no está se marca', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      ExposureReport(
        measured: true,
        measuredAt: DateTime(2026, 8, 4, 20, 14, 3),
        gate: GateState.present,
        ports: const <ExposedPort>[
          ExposedPort(
            proto: 'udp',
            from: 16261,
            to: 16261,
            applied: false,
            isControl: false,
            reachableBy: <String>['100.64.1.5'],
          ),
        ],
      ),
    );

    // Es la fila que explica por qué un amigo se queda fuera. Sin ella el
    // síntoma es "a mí no me conecta" sin nada que mirar.
    expect(find.text('no está puesto'), findsOneWidget);
  });

  testWidgets('el hueco del canal se distingue de un puerto de juego', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      ExposureReport(
        measured: true,
        measuredAt: DateTime(2026, 8, 4, 20, 14, 3),
        gate: GateState.present,
        ports: const <ExposedPort>[
          juego,
          ExposedPort(
            proto: 'tcp',
            from: 57623,
            to: 57623,
            applied: true,
            isControl: true,
            reachableBy: <String>['100.64.1.5'],
          ),
        ],
      ),
    );

    // El usuario no va a encontrar este puerto en el perfil de su juego, así
    // que enseñarlo igual que los otros lo manda a buscar algo que no está.
    expect(find.textContaining('Canal de la sala'), findsOneWidget);
    expect(find.textContaining('Abierto para 100.64.1.5'), findsOneWidget);
  });

  testWidgets('una regla que nadie pidió se denuncia', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      ExposureReport(
        measured: true,
        measuredAt: DateTime(2026, 8, 4, 20, 14, 3),
        gate: GateState.present,
        ports: const <ExposedPort>[juego],
        unexpected: const <String>['kanpachi-regla-huerfana'],
      ),
    );

    expect(find.textContaining('kanpachi-regla-huerfana'), findsOneWidget);
  });
}
