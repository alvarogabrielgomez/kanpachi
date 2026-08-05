import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/probe_section.dart';
import 'package:kanpachi_ui/features/session/domain/entities/probe.dart';

/// La comprobación que sale a la red.
///
/// Lo que se afirma acá no es cómo se ve: es que **no puede afirmar de más**.
/// Un sondeo que no alcanzó la otra máquina no se puede pintar como "cerrado", y
/// un puerto callado no se puede llamar cerrado, porque está medido que en
/// Windows el silencio no distingue "bloqueado" de "no hay nada escuchando".
void main() {
  Future<void> pinta(
    WidgetTester tester, {
    required Future<ProbeReport> Function() run,
    bool isHost = false,
    String hostName = 'alvaro',
  }) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: Scaffold(
          body: SingleChildScrollView(
            child: ProbeSection(run: run, isHost: isHost, hostName: hostName),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  ProbeReport informe(List<ProbeResult> results, ProbeVerdict verdict) =>
      ProbeReport(
        measured: true,
        verdict: verdict,
        target: '100.64.1.1',
        name: 'alvaro',
        measuredAt: DateTime(2026, 8, 4, 21, 30, 15),
        results: results,
      );

  const ProbeResult canal = ProbeResult(
    port: 57623,
    kind: ProbeKind.reference,
    label: 'el canal de la sala',
    outcome: ProbeOutcome.answered,
    rttMs: 11,
  );

  testWidgets('antes de pulsar no dice que esté cerrado', (
    WidgetTester tester,
  ) async {
    await pinta(tester, run: () async => const ProbeReport.blind());

    expect(find.textContaining('Todavía no se ha probado'), findsOneWidget);
    expect(
      find.textContaining('no se alcanza nada más'),
      findsNothing,
      reason:
          'sin haber marcado nada, la pantalla no puede afirmar que está cerrado',
    );
  });

  testWidgets('el host no tiene botón, y se le dice por qué', (
    WidgetTester tester,
  ) async {
    bool lanzado = false;
    await pinta(
      tester,
      isHost: true,
      run: () async {
        lanzado = true;
        return const ProbeReport.blind();
      },
    );

    expect(
      find.textContaining('lo tiene que pulsar alguien más'),
      findsOneWidget,
    );
    expect(find.byType(AppButton), findsNothing);
    expect(find.textContaining('Probar la PC'), findsNothing);
    expect(
      lanzado,
      isFalse,
      reason:
          'sondearse a uno mismo no atraviesa el firewall y diría que está '
          'todo abierto en una máquina blindada',
    );
  });

  testWidgets('una fuga se nombra con el puerto que contestó', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      run: () async => informe(const <ProbeResult>[
        canal,
        ProbeResult(
          port: 445,
          kind: ProbeKind.forbidden,
          label: 'compartir archivos (SMB)',
          outcome: ProbeOutcome.answered,
          rttMs: 9,
        ),
      ], ProbeVerdict.leaky),
    );
    await tester.tap(find.textContaining('Probar la PC de alvaro'));
    await tester.pumpAndSettle();

    expect(
      find.textContaining('se llega a algo que Kanpachi no abrió'),
      findsOneWidget,
    );
    // Decir "hay algo abierto" sin decir qué manda al usuario a buscar a ciegas.
    expect(find.textContaining('445 · compartir archivos (SMB)'), findsWidgets);
  });

  testWidgets('sin respuesta del canal no se afirma que esté cerrado', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      run: () async => informe(const <ProbeResult>[
        ProbeResult(
          port: 57623,
          kind: ProbeKind.reference,
          label: 'el canal de la sala',
          outcome: ProbeOutcome.silent,
        ),
        ProbeResult(
          port: 445,
          kind: ProbeKind.forbidden,
          label: 'compartir archivos (SMB)',
          outcome: ProbeOutcome.silent,
        ),
      ], ProbeVerdict.unreachable),
    );
    await tester.tap(find.textContaining('Probar la PC de alvaro'));
    await tester.pumpAndSettle();

    // Todo callado se ve igual con la PC blindada y con la PC apagada.
    expect(find.textContaining('No se pudo alcanzar esa PC'), findsOneWidget);
    expect(find.textContaining('no se alcanza nada más'), findsNothing);
  });

  testWidgets(
    'un puerto callado se enseña como sin respuesta, jamás como cerrado',
    (WidgetTester tester) async {
      await pinta(
        tester,
        run: () async => informe(const <ProbeResult>[
          canal,
          ProbeResult(
            port: 3389,
            kind: ProbeKind.forbidden,
            label: 'Escritorio remoto',
            outcome: ProbeOutcome.silent,
          ),
        ], ProbeVerdict.sealed),
      );
      await tester.tap(find.textContaining('Probar la PC de alvaro'));
      await tester.pumpAndSettle();

      expect(find.text('sin respuesta'), findsOneWidget);
      expect(
        find.text('cerrado'),
        findsNothing,
        reason:
            'en Windows el silencio no distingue bloqueado de nada escuchando, '
            'así que esa fila no puede llamarse cerrada',
      );
      // Y la fila dice por qué está cada puerto en la lista: sin eso, el canal
      // contestando se lee igual que SMB contestando, y son lo contrario.
      expect(find.textContaining('Tiene que contestar'), findsOneWidget);
      expect(find.text('No tiene que contestar'), findsOneWidget);
    },
  );

  testWidgets('el veredicto limpio lleva su advertencia de alcance', (
    WidgetTester tester,
  ) async {
    await pinta(
      tester,
      run: () async => informe(const <ProbeResult>[canal], ProbeVerdict.sealed),
    );
    await tester.tap(find.textContaining('Probar la PC de alvaro'));
    await tester.pumpAndSettle();

    expect(find.textContaining('no se alcanza nada más'), findsOneWidget);
    // Sin esta frase, "no se alcanza nada" se lee como "no hay nada abierto", y
    // es falso: se prueban unos cuantos puertos TCP, no todos.
    expect(find.textContaining('no todos'), findsOneWidget);
  });

  testWidgets('un fallo se enseña y no deja el resultado viejo en pantalla', (
    WidgetTester tester,
  ) async {
    bool primera = true;
    await pinta(
      tester,
      run: () async {
        if (primera) {
          primera = false;
          return informe(const <ProbeResult>[canal], ProbeVerdict.sealed);
        }
        throw Exception('el host se fue');
      },
    );

    await tester.tap(find.textContaining('Probar la PC de alvaro'));
    await tester.pumpAndSettle();
    expect(find.textContaining('no se alcanza nada más'), findsOneWidget);

    await tester.tap(find.textContaining('Probar la PC de alvaro'));
    await tester.pumpAndSettle();

    expect(find.textContaining('No se pudo probar'), findsOneWidget);
    expect(
      find.textContaining('no se alcanza nada más'),
      findsNothing,
      reason:
          'un resultado viejo al lado de un error nuevo se lee como si '
          'siguiera valiendo',
    );
  });
}
