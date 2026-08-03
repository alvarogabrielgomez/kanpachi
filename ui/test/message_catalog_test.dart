import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/messages/app_message.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';

/// Los candados del catálogo.
///
/// El `switch` sin `default` ya obliga a que cada clave tenga una rama, así que
/// el compilador cubre la mitad del problema: no se puede olvidar una. Lo que
/// el compilador NO puede ver es que la rama diga algo útil. Una rama que
/// devuelva `AppMessage.none` compila igual, y el aviso desaparece de la
/// pantalla sin que nada falle.
///
/// Estos tests cubren esa otra mitad, más la regla de microcopy del producto,
/// que es la razón por la que el catálogo existe.
void main() {
  group('toda clave del daemon tiene texto', () {
    test('cada alerta trae título y cuerpo', () {
      for (final AlertKind kind in AlertKind.values) {
        final AppMessage m = AppMessages.alert(kind);

        expect(
          m.isEmpty,
          isFalse,
          reason: 'la alerta ${kind.wire} no tiene texto. El módulo de '
              'exposición la levantó y la pantalla no mostraría nada.',
        );
        expect(
          m.title,
          isNotNull,
          reason: 'la alerta ${kind.wire} no dice QUÉ pasó',
        );
        expect(
          m.body,
          isNotEmpty,
          reason: 'la alerta ${kind.wire} dice qué pasó y no qué significa. '
              'Un aviso así deja al usuario con el susto y sin salida.',
        );
      }
    });

    test('cada código de error trae cuerpo', () {
      for (final FailureCode code in FailureCode.values) {
        expect(
          AppMessages.failure(code).body,
          isNotEmpty,
          reason: 'el código ${code.wire} no tiene texto, así que la operación '
              'fallaría en silencio',
        );
      }
    });

    test('cada motivo de salida trae texto, salvo el de salir por tu cuenta',
        () {
      for (final ExitReason reason in ExitReason.values) {
        final AppMessage m = AppMessages.exit(reason);

        if (reason == ExitReason.user) {
          // El único vacío a propósito. docs/05-ui.md: "Sin texto. No hay nada
          // que explicar".
          expect(m.isEmpty, isTrue, reason: 'salir por tu cuenta no se explica');
          continue;
        }
        expect(
          m.isEmpty,
          isFalse,
          reason: 'volver a la portada por ${reason.wire} no dice nada, así '
              'que se ve igual que haber salido por tu cuenta',
        );
      }
    });
  });

  group('el estado de la conexión', () {
    test('solo los dos que piden atención llevan texto', () {
      // Una línea que diga "conectando" encima de una pantalla que ya dice
      // "conectando" es ruido, así que el silencio de los cuatro primeros es
      // parte del diseño y no un olvido.
      const Set<ConnState> conAviso = <ConnState>{
        ConnState.degraded,
        ConnState.reconnecting,
      };

      for (final ConnState state in ConnState.values) {
        final bool tieneTexto = !AppMessages.connection(state).isEmpty;
        expect(
          tieneTexto,
          equals(conAviso.contains(state)),
          reason: tieneTexto
              ? 'el estado ${state.wire} pinta un aviso que no hace falta'
              : 'el estado ${state.wire} deja al usuario sin saber qué pasa',
        );
      }
    });
  });

  group('la regla de microcopy', () {
    /// docs/05-ui.md: "cada error dice qué pasó, qué significa para el usuario
    /// y qué hacer, en ese orden, en español coloquial. Jamás un código de
    /// error a secas".
    test('ningún texto filtra la clave del cable ni jerga del daemon', () {
      final List<AppMessage> todos = <AppMessage>[
        ...AlertKind.values.map(AppMessages.alert),
        ...FailureCode.values.map(AppMessages.failure),
        ...ExitReason.values.map(AppMessages.exit),
        AppMessages.unknown,
      ];

      // Las claves son de máquina y el usuario no puede hacer nada con ellas.
      final Set<String> claves = <String>{
        ...AlertKind.values.map((AlertKind k) => k.wire),
        ...FailureCode.values.map((FailureCode c) => c.wire),
      };

      for (final AppMessage m in todos) {
        final String texto = '${m.title ?? ''} ${m.body}';
        for (final String clave in claves) {
          expect(
            texto.contains(clave),
            isFalse,
            reason: 'el texto "$texto" lleva la clave $clave dentro. La regla '
                'del producto es que nunca se muestra un código a secas.',
          );
        }
      }
    });

    test('los textos están redactados, no truncados', () {
      for (final AlertKind kind in AlertKind.values) {
        final AppMessage m = AppMessages.alert(kind);
        expect(
          m.body.trim().length,
          greaterThan(20),
          reason: 'el cuerpo de ${kind.wire} es demasiado corto para explicar '
              'qué significa y qué hacer',
        );
      }
    });
  });

  group('lo que llega crudo del cable', () {
    test('una clave desconocida sale como el mensaje de reserva, no en blanco',
        () {
      final AppMessage m = AppMessages.alertFromWire('inventada_en_el_futuro');

      expect(m, equals(AppMessages.unknown));
      expect(
        m.isEmpty,
        isFalse,
        reason: 'un daemon más nuevo que la app dejaría la pantalla muda',
      );
    });

    test('una clave conocida se resuelve igual que por enum', () {
      for (final AlertKind kind in AlertKind.values) {
        expect(
          AppMessages.alertFromWire(kind.wire),
          equals(AppMessages.alert(kind)),
          reason: 'la clave ${kind.wire} se resuelve distinto según por dónde '
              'entre',
        );
      }
    });

    test('sin motivo de salida no se inventa ninguno', () {
      // Es lo que pasa al arrancar por primera vez: nunca hubo sesión anterior.
      expect(AppMessages.exitFromWire(null), equals(AppMessage.none));
      expect(AppMessages.exitFromWire(''), equals(AppMessage.none));
    });

    test('un código de error desconocido no se traga', () {
      expect(AppMessages.failureFromWire('codigo_del_futuro'),
          equals(AppMessages.unknown));
    });
  });

  group('el detalle del daemon', () {
    test('acompaña al mensaje sin sustituir el cuerpo', () {
      const String detalle = 'el router publica el puerto 25565 hacia 192.168.1.7';
      final AppMessage conDetalle =
          AppMessages.alert(AlertKind.routerMapping, detail: detalle);
      final AppMessage sinDetalle = AppMessages.alert(AlertKind.routerMapping);

      expect(conDetalle.detail, equals(detalle));
      expect(
        conDetalle.body,
        equals(sinDetalle.body),
        reason: 'el copy lo decide la clave; el detalle del daemon acompaña. '
            'Si lo sustituyera, el texto del producto dependería de cómo '
            'redacte el daemon.',
      );
    });

    test('un detalle vacío no deja el campo puesto', () {
      expect(AppMessages.alert(AlertKind.firewallOff, detail: '').detail, isNull);
      expect(
          AppMessages.alert(AlertKind.firewallOff, detail: null).detail, isNull);
    });
  });
}
