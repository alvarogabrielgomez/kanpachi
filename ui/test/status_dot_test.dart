import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';

/// El punto hueco, que es el segundo canal del color.
///
/// El estado que lo estrena es el del servidor del juego levantado donde la
/// sala no lo alcanza: no está roto y no funciona. Antes era un ámbar lleno,
/// indistinguible del verde para quien no separa rojo de verde, y la app le
/// pedía a esa persona que leyera un tooltip para saber si su partida servía.
void main() {
  Future<BoxDecoration> pintar(WidgetTester tester, Widget punto) async {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(body: Center(child: punto)),
      ),
    );
    final Container c = tester.widget<Container>(
      find.descendant(
        of: find.byType(AppStatusDot),
        matching: find.byType(Container),
      ),
    );
    return c.decoration! as BoxDecoration;
  }

  testWidgets('lleno por omisión: pinta el interior y no lleva borde', (
    WidgetTester tester,
  ) async {
    final BoxDecoration d = await pintar(
      tester,
      const AppStatusDot(color: Color(0xFF89D268)),
    );

    expect(d.color, const Color(0xFF89D268));
    expect(d.border, isNull);
  });

  testWidgets('hueco: el color se va al borde y el interior queda vacío', (
    WidgetTester tester,
  ) async {
    final BoxDecoration d = await pintar(
      tester,
      const AppStatusDot(color: Color(0xFFF2913C), filled: false),
    );

    expect(d.color, Colors.transparent);
    expect(d.border, isNotNull);
    expect((d.border! as Border).top.color, const Color(0xFFF2913C));
  });

  testWidgets(
    'el borde crece con el punto, para que el hueco siga siendo hueco',
    (WidgetTester tester) async {
      final BoxDecoration chico = await pintar(
        tester,
        const AppStatusDot(color: Color(0xFFF2913C), filled: false, size: 7),
      );
      final BoxDecoration grande = await pintar(
        tester,
        const AppStatusDot(color: Color(0xFFF2913C), filled: false, size: 14),
      );

      final double a = (chico.border! as Border).top.width;
      final double b = (grande.border! as Border).top.width;
      expect(b, greaterThan(a));
    },
  );
}
