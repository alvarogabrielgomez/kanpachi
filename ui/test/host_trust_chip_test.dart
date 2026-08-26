import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/features/seed/presentation/widgets/host_trust_chip.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';

/// La continuidad del host cabe en una etiqueta, y el detalle vive en su
/// tooltip.
///
/// Lo que se afirma acá no es cómo se ve: es que la etiqueta **no puede decir
/// que conoce una llave que no conoce**. Los cuatro veredictos que el daemon
/// sabe contestar tienen que llegar a cuatro frases distintas, y el que no
/// verificó nada no puede pintar ninguna.
void main() {
  Future<void> pinta(WidgetTester tester, PendingInvite invite) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: Center(child: HostTrustChip(invite: invite)),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  PendingInvite conVeredicto(HostVerdict v, {int salas = 0}) => PendingInvite(
    link: 'kanpachi://A7K2-M9QX@servidor',
    code: 'A7K2-M9QX',
    seed: 'kanpachi.accentio.dev',
    fingerprint: '5502 6194 5464 5033 2100',
    verdict: v,
    knownFingerprint: '5502 6194 5464 5033 2100',
    knownRooms: salas,
  );

  testWidgets('una llave nunca vista dice que es nueva', (
    WidgetTester tester,
  ) async {
    await pinta(tester, conVeredicto(HostVerdict.nueva));

    expect(find.text('Host nuevo'), findsOneWidget);
    expect(
      tester.widget<Tooltip>(find.byType(Tooltip)).message,
      contains('primera vez'),
    );
  });

  testWidgets('una llave de siempre dice que se conoce', (
    WidgetTester tester,
  ) async {
    await pinta(tester, conVeredicto(HostVerdict.conocida, salas: 126));

    expect(find.text('Host conocido'), findsOneWidget);
    // La cuenta de salas no sale en NINGUNA de las tres capas. No cambia
    // ninguna decisión y solo pide sitio.
    expect(find.textContaining('126'), findsNothing);
    expect(
      tester.widget<Tooltip>(find.byType(Tooltip)).message,
      isNot(contains('126')),
    );
    expect(
      tester.widget<Tooltip>(find.byType(Tooltip)).message,
      contains('Ya entraste'),
    );
  });

  testWidgets('la misma llave con otro nombre sigue siendo conocida', (
    WidgetTester tester,
  ) async {
    await pinta(tester, conVeredicto(HostVerdict.renombrada, salas: 3));

    expect(find.text('Host conocido'), findsOneWidget);
    expect(
      tester.widget<Tooltip>(find.byType(Tooltip)).message,
      contains('nombre'),
    );
  });

  testWidgets('un nombre conocido con otra llave lo dice, y manda a comparar', (
    WidgetTester tester,
  ) async {
    await pinta(tester, conVeredicto(HostVerdict.llaveCambiada, salas: 4));

    expect(find.text('La llave cambió'), findsOneWidget);
    expect(
      tester.widget<Tooltip>(find.byType(Tooltip)).message,
      contains('huellas'),
    );
  });

  testWidgets('sin nada comprobado no se pinta ninguna etiqueta', (
    WidgetTester tester,
  ) async {
    await pinta(tester, conVeredicto(HostVerdict.unverified));

    expect(find.byType(Tooltip), findsNothing);
    expect(find.textContaining('Host'), findsNothing);
  });

  testWidgets(
    'pulsar la etiqueta enseña la huella, y pulsar fuera la esconde',
    (WidgetTester tester) async {
      await pinta(tester, conVeredicto(HostVerdict.conocida, salas: 126));

      expect(find.text('5502 6194 5464 5033 2100'), findsNothing);

      await tester.tap(find.byType(HostTrustChip));
      await tester.pumpAndSettle();
      expect(find.text('5502 6194 5464 5033 2100'), findsOneWidget);

      // Fuera, y no otra vez encima: el panel abierto tiende una capa sobre
      // toda la pantalla para cerrarse, así que la píldora queda debajo de
      // ella. Es lo que se espera de algo que se abrió pulsando.
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();
      expect(find.text('5502 6194 5464 5033 2100'), findsNothing);
    },
  );

  testWidgets('con la llave cambiada el panel trae las dos huellas', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      PendingInvite(
        link: 'kanpachi://A7K2-M9QX@servidor',
        fingerprint: '1111 2222 3333 4444 5555',
        verdict: HostVerdict.llaveCambiada,
        knownFingerprint: '5502 6194 5464 5033 2100',
        knownRooms: 4,
      ),
    );

    await tester.tap(find.byType(HostTrustChip));
    await tester.pumpAndSettle();

    // Con etiqueta las dos, porque lo que se pide es compararlas y sin
    // nombre no se sabe cuál es cuál.
    expect(find.text('ANTES'), findsOneWidget);
    expect(find.text('AHORA'), findsOneWidget);
    expect(find.text('5502 6194 5464 5033 2100'), findsOneWidget);
    expect(find.text('1111 2222 3333 4444 5555'), findsOneWidget);
  });
}
