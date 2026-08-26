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
    // La cuenta se va del sitio donde estorbaba y se queda donde se busca.
    expect(find.textContaining('126'), findsNothing);
    expect(
      tester.widget<Tooltip>(find.byType(Tooltip)).message,
      contains('126'),
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
}
