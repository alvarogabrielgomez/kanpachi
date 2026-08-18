import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/seed/domain/seed_password.dart';

/// El candado que hace verdadera la frase "espejo del backend".
///
/// # Qué pasa sin esto
///
/// Los enums de `message_keys.dart` copian a mano las cadenas que el daemon
/// manda por el cable. Una copia a mano no se entera cuando el original cambia:
/// si alguien agrega una alerta en Go, acá no falla nada. La clave nueva llega,
/// `fromWire` devuelve `null`, el catálogo saca el mensaje de reserva, y el
/// aviso que el daemon se molestó en levantar sale como "algo que esta versión
/// no sabe explicar".
///
/// Nadie lo vería en desarrollo, porque el repositorio falso no manda alertas.
///
/// # Cómo lo comprueba
///
/// Leyendo el fuente de Go y sacando las cadenas de las funciones que las
/// escriben. Es feo y es lo correcto: el contrato de verdad son esas funciones,
/// no un documento que las describa. Un test que comparase esto contra una
/// tercera lista escrita a mano solo movería el problema de sitio.
///
/// Ya pasó una vez del lado de Go, con `audit_failed`: la constante existía en
/// el dominio y `alertName` no la nombraba, así que la alerta viajaba como
/// "unknown". Del lado de Dart lo vigila esto.
void main() {
  final Directory repo = Directory.current.parent;

  group('los enums del cable no se quedan atrás del daemon', () {
    test('AlertKind tiene las mismas claves que alertName en Go', () {
      final Set<String> enGo = _cadenasDeLaFuncion(
        repo: repo,
        archivo: 'daemon/transport/protocol/view.go',
        funcion: 'alertName',
      );
      final Set<String> enDart = AlertKind.values
          .map((AlertKind k) => k.wire)
          .toSet();

      expect(
        enGo,
        isNotEmpty,
        reason:
            'no se leyó ninguna clave de alertName: la ruta cambió y este '
            'test no está vigilando nada',
      );
      expect(
        enDart,
        equals(enGo),
        reason:
            'AlertKind y alertName dejaron de coincidir. Lo que sobra en '
            'Dart es una clave que el daemon ya no manda; lo que falta llega '
            'como mensaje de reserva y el aviso se pierde.',
      );
    });

    test('RuleClass tiene las mismas claves que ruleClassName en Go', () {
      final Set<String> enGo = _cadenasDeLaFuncion(
        repo: repo,
        archivo: 'daemon/transport/protocol/params.go',
        funcion: 'ruleClassName',
      );
      final Set<String> enDart = RuleClass.values
          .map((RuleClass c) => c.wire)
          .toSet();

      expect(
        enGo,
        isNotEmpty,
        reason:
            'no se leyó ninguna clave de ruleClassName: la ruta cambió y '
            'este test no está vigilando nada',
      );
      expect(
        enDart,
        equals(enGo),
        reason:
            'RuleClass y ruleClassName dejaron de coincidir. Una clase que '
            'la UI no conoce cae en el valor de reserva, y aunque ese es el '
            'lado seguro, el usuario lee un texto que no describe lo que tiene.',
      );
    });

    test('ExitReason tiene los mismos motivos que exitName en Go', () {
      final Set<String> enGo = _cadenasDeLaFuncion(
        repo: repo,
        archivo: 'daemon/transport/protocol/view.go',
        funcion: 'exitName',
      );
      final Set<String> enDart = ExitReason.values
          .map((ExitReason r) => r.wire)
          .toSet();

      expect(enGo, isNotEmpty, reason: 'no se leyó ningún motivo de exitName');
      expect(
        enDart,
        equals(enGo),
        reason:
            'ExitReason y exitName dejaron de coincidir. Sin el motivo, '
            'que te expulsen y salir por tu cuenta se ven igual.',
      );
    });

    test('ConnState tiene los mismos estados que connName en Go', () {
      final Set<String> enGo = _cadenasDeLaFuncion(
        repo: repo,
        archivo: 'daemon/transport/protocol/view.go',
        funcion: 'connName',
      );
      final Set<String> enDart = ConnState.values
          .map((ConnState s) => s.wire)
          .toSet();

      expect(enGo, isNotEmpty, reason: 'no se leyó ningún estado de connName');
      expect(
        enDart,
        equals(enGo),
        reason:
            'ConnState y connName dejaron de coincidir. Un estado que la '
            'UI no conoce deja la pantalla sin saber si está dentro, '
            'reconectando o fuera.',
      );
    });

    test('GateState tiene los mismos estados que gateName en Go', () {
      final Set<String> enGo = _cadenasDeLaFuncion(
        repo: repo,
        archivo: 'daemon/transport/protocol/view.go',
        funcion: 'gateName',
      );
      final Set<String> enDart = GateState.values
          .map((GateState s) => s.wire)
          .toSet();

      expect(enGo, isNotEmpty, reason: 'no se leyó ningún estado de gateName');
      expect(
        enDart,
        equals(enGo),
        reason:
            'GateState y gateName dejaron de coincidir. La segunda fila de '
            'la pantalla de exposición es la que dice que todo lo demás está '
            'cerrado, y un estado que la UI no conoce la deja en blanco.',
      );
    });

    test('ProbeVerdict tiene los mismos veredictos que verdictName en Go', () {
      final Set<String> enGo = _cadenasDeLaFuncion(
        repo: repo,
        archivo: 'daemon/transport/protocol/view.go',
        funcion: 'verdictName',
      );
      final Set<String> enDart = ProbeVerdict.values
          .map((ProbeVerdict v) => v.wire)
          .toSet();

      expect(
        enGo,
        isNotEmpty,
        reason: 'no se leyó ningún veredicto de verdictName',
      );
      expect(
        enDart,
        equals(enGo),
        reason:
            'ProbeVerdict y verdictName dejaron de coincidir. El veredicto '
            'es lo que decide si la pantalla dice "cerrado", "no se pudo '
            'alcanzar" o enseña una alarma, y son cosas muy distintas.',
      );
    });

    test('ProbeKind tiene las mismas clases que probeKindName en Go', () {
      final Set<String> enGo = _cadenasDeLaFuncion(
        repo: repo,
        archivo: 'daemon/transport/protocol/view.go',
        funcion: 'probeKindName',
      );
      final Set<String> enDart = ProbeKind.values
          .map((ProbeKind k) => k.wire)
          .toSet();

      expect(
        enGo,
        isNotEmpty,
        reason: 'no se leyó ninguna clase de probeKindName',
      );
      expect(
        enDart,
        equals(enGo),
        reason:
            'ProbeKind y probeKindName dejaron de coincidir. Una clase que '
            'la UI no conoce cae en "prohibido", que hace ruido de más pero no '
            'esconde nada; que falte al revés sí esconde.',
      );
    });

    test(
      'ProbeOutcome tiene los mismos resultados que probeOutcomeName en Go',
      () {
        final Set<String> enGo = _cadenasDeLaFuncion(
          repo: repo,
          archivo: 'daemon/transport/protocol/view.go',
          funcion: 'probeOutcomeName',
        );
        final Set<String> enDart = ProbeOutcome.values
            .map((ProbeOutcome o) => o.wire)
            .toSet();

        expect(
          enGo,
          isNotEmpty,
          reason: 'no se leyó ningún resultado de probeOutcomeName',
        );
        expect(
          enDart,
          equals(enGo),
          reason:
              'ProbeOutcome y probeOutcomeName dejaron de coincidir. Un '
              'resultado que la UI no conoce cae en "no se pudo preguntar", que '
              'es lo único que no se puede leer como "está cerrado".',
        );
      },
    );

    test(
      'CanaryVerdict tiene los mismos veredictos que canaryVerdictName en Go',
      () {
        final Set<String> enGo = _cadenasDeLaFuncion(
          repo: repo,
          archivo: 'daemon/transport/protocol/view.go',
          funcion: 'canaryVerdictName',
        );
        final Set<String> enDart = CanaryVerdict.values
            .map((CanaryVerdict v) => v.wire)
            .toSet();

        expect(
          enGo,
          isNotEmpty,
          reason: 'no se leyó ningún veredicto de canaryVerdictName',
        );
        expect(
          enDart,
          equals(enGo),
          reason:
              'CanaryVerdict y canaryVerdictName dejaron de coincidir. Un '
              'veredicto que la UI no conoce cae en "sin comprobar", y eso deja '
              'una fuga demostrada sin pintar en la pantalla.',
        );
      },
    );

    test(
      'QuarantineVerdict tiene los mismos veredictos que quarantineVerdictName en Go',
      () {
        final Set<String> enGo = _cadenasDeLaFuncion(
          repo: repo,
          archivo: 'daemon/transport/protocol/view.go',
          funcion: 'quarantineVerdictName',
        );
        final Set<String> enDart = QuarantineVerdict.values
            .map((QuarantineVerdict v) => v.wire)
            .toSet();

        expect(
          enGo,
          isNotEmpty,
          reason: 'no se leyó ningún veredicto de quarantineVerdictName',
        );
        expect(
          enDart,
          equals(enGo),
          reason:
              'QuarantineVerdict y quarantineVerdictName dejaron de coincidir. '
              'Un veredicto que la UI no conoce cae en "no se pudo medir", y '
              'el interruptor de Configuración se apaga en gris sin explicar '
              'nada.',
        );
      },
    );

    test(
      'QuarantineDecision tiene las mismas respuestas que quarantineDecisionName en Go',
      () {
        final Set<String> enGo = _cadenasDeLaFuncion(
          repo: repo,
          archivo: 'daemon/transport/protocol/view.go',
          funcion: 'quarantineDecisionName',
        );
        final Set<String> enDart = QuarantineDecision.values
            .map((QuarantineDecision d) => d.wire)
            .toSet();

        expect(
          enGo,
          isNotEmpty,
          reason: 'no se leyó ninguna respuesta de quarantineDecisionName',
        );
        expect(
          enDart,
          equals(enGo),
          reason:
              'QuarantineDecision y quarantineDecisionName dejaron de '
              'coincidir. Una respuesta que la UI no conoce cae en "sin '
              'decidir", y la pantalla volvería a tratar como debida una '
              'pregunta que ya se contestó.',
        );
      },
    );

    test('FailureCode tiene los mismos códigos que protocol.Code en Go', () {
      final Set<String> enGo = _codigosDelProtocolo(repo);
      final Set<String> enDart = FailureCode.values
          .map((FailureCode c) => c.wire)
          .toSet();

      expect(enGo, isNotEmpty, reason: 'no se leyó ningún Code de protocol.go');
      expect(
        enDart,
        equals(enGo),
        reason:
            'FailureCode y protocol.Code dejaron de coincidir. La casa '
            'decide por código y no por texto, así que un código que la UI no '
            'conoce es una pantalla que no sabe qué decir.',
      );
    });
  });

  test('ninguna clave del cable está repetida', () {
    void sinRepetidos(String nombre, List<String> claves) {
      expect(
        claves.toSet().length,
        equals(claves.length),
        reason: '$nombre tiene claves repetidas: $claves',
      );
    }

    sinRepetidos(
      'AlertKind',
      AlertKind.values.map((AlertKind k) => k.wire).toList(),
    );
    sinRepetidos(
      'RuleClass',
      RuleClass.values.map((RuleClass c) => c.wire).toList(),
    );
    sinRepetidos(
      'ExitReason',
      ExitReason.values.map((ExitReason r) => r.wire).toList(),
    );
    sinRepetidos(
      'FailureCode',
      FailureCode.values.map((FailureCode c) => c.wire).toList(),
    );
    sinRepetidos(
      'CanaryVerdict',
      CanaryVerdict.values.map((CanaryVerdict v) => v.wire).toList(),
    );
    sinRepetidos(
      'QuarantineVerdict',
      QuarantineVerdict.values.map((QuarantineVerdict v) => v.wire).toList(),
    );
    sinRepetidos(
      'QuarantineDecision',
      QuarantineDecision.values.map((QuarantineDecision d) => d.wire).toList(),
    );
  });

  // Los NOMBRES de método, que hasta ahora no los vigilaba nada.
  //
  // Los enums del cable llevaban candado y esto no, y se notó al agregar
  // `own_seed`: la constante entró en Go y en Dart no se puso rojo nada. El
  // fallo que deja pasar es peor que el de un enum, porque no degrada: un
  // método que la interfaz nombra mal no sale como mensaje de reserva, sale
  // como `bad_request` en mitad de una operación que la persona pidió.
  group('los nombres de método no se quedan atrás del daemon', () {
    test('DaemonMethods no nombra ninguno que el daemon no tenga', () {
      final Set<String> enGo = _metodosDelProtocolo(repo);
      final Set<String> enDart = _metodosDeDart();

      expect(
        enGo,
        isNotEmpty,
        reason:
            'no se leyó ningún método de protocol.go: la ruta cambió y este '
            'test no está vigilando nada',
      );
      expect(
        enDart.difference(enGo),
        isEmpty,
        reason:
            'la interfaz llama a métodos que el daemon no conoce. La lista de '
            'métodos es CERRADA, así que eso no degrada: es un bad_request en '
            'mitad de lo que alguien pidió',
      );
    });

    // La dirección contraria NO se exige, y hay que decir por qué: el daemon
    // tiene métodos que solo usa el CLI, y va a seguir teniéndolos. Exigir
    // igualdad obligaría a la interfaz a declarar constantes que no llama, que
    // es código muerto puesto para que un test calle.
    test('la interfaz nombra los métodos que de verdad usa', () {
      expect(_metodosDeDart(), contains('seed_password'));
      expect(_metodosDeDart(), contains('own_seed'));
    });
  });

  // La regla del password, que vive en los dos lados a la fuerza.
  //
  // La interfaz tiene que decidir si habilita el botón antes de hablar con
  // nadie. Separadas, la app acepta lo que el daemon rechaza y quien lo escribe
  // concluye que la app está rota.
  group('la regla del password del registro es la misma en los dos lados', () {
    test('SeedPassword espeja las constantes de core/domain', () {
      final int minGo = _constanteDeGo(
        repo: repo,
        archivo: 'core/domain/seedauth.go',
        nombre: 'MinSeedPasswordLen',
      );
      final int maxGo = _constanteDeGo(
        repo: repo,
        archivo: 'core/domain/seedauth.go',
        nombre: 'MaxSeedPasswordLen',
      );

      expect(
        SeedPassword.minLength,
        equals(minGo),
        reason:
            'el mínimo se separó: la app habilita el botón con algo que el '
            'daemon rechaza, o al revés',
      );
      expect(SeedPassword.maxLength, equals(maxGo));
    });
  });
}

/// Saca las cadenas devueltas dentro de una función de Go.
///
/// Recorta desde `func <nombre>(` hasta el `default:`, que es donde termina lo
/// que nos interesa: la cadena del `default` es el hueco ("unknown", ""), no
/// una clave del contrato.
Set<String> _cadenasDeLaFuncion({
  required Directory repo,
  required String archivo,
  required String funcion,
}) {
  final File fuente = File('${repo.path}/$archivo');
  if (!fuente.existsSync()) {
    fail('no se encuentra $archivo desde ${repo.path}');
  }

  final String texto = fuente.readAsStringSync();
  final int inicio = texto.indexOf('func $funcion(');
  if (inicio < 0) fail('no se encontró func $funcion en $archivo');

  final int corte = texto.indexOf('default:', inicio);
  final int fin = corte < 0 ? texto.length : corte;

  return RegExp('return "([a-z_]+)"')
      .allMatches(texto.substring(inicio, fin))
      .map((RegExpMatch m) => m.group(1)!)
      .toSet();
}

/// Saca los valores del bloque de constantes `Code` de protocol.go.
Set<String> _codigosDelProtocolo(Directory repo) {
  final File fuente = File(
    '${repo.path}/daemon/transport/protocol/protocol.go',
  );
  if (!fuente.existsSync()) {
    fail('no se encuentra protocol.go desde ${repo.path}');
  }

  return RegExp(r'Code\s*=\s*"([a-z_]+)"')
      .allMatches(fuente.readAsStringSync())
      .map((RegExpMatch m) => m.group(1)!)
      .toSet();
}

/// Saca los valores del bloque de constantes `Method` de protocol.go.
Set<String> _metodosDelProtocolo(Directory repo) {
  final File fuente = File(
    '${repo.path}/daemon/transport/protocol/protocol.go',
  );
  if (!fuente.existsSync()) {
    fail('no se encuentra protocol.go desde ${repo.path}');
  }

  return RegExp(r'Method\s*=\s*"([a-z_]+)"')
      .allMatches(fuente.readAsStringSync())
      .map((RegExpMatch m) => m.group(1)!)
      .toSet();
}

/// Saca las constantes de `DaemonMethods`.
Set<String> _metodosDeDart() {
  final File fuente = File(
    'lib/features/session/infra/daemon/daemon_methods.dart',
  );
  if (!fuente.existsSync()) {
    fail('no se encuentra daemon_methods.dart');
  }

  return RegExp(r"static const String \w+ = '([a-z_]+)';")
      .allMatches(fuente.readAsStringSync())
      .map((RegExpMatch m) => m.group(1)!)
      .toSet();
}

/// Saca un entero de una constante de Go, por nombre.
int _constanteDeGo({
  required Directory repo,
  required String archivo,
  required String nombre,
}) {
  final File fuente = File('${repo.path}/$archivo');
  if (!fuente.existsSync()) {
    fail('no se encuentra $archivo desde ${repo.path}');
  }
  final RegExpMatch? m = RegExp(
    nombre + r'\s*=\s*(\d+)',
  ).firstMatch(fuente.readAsStringSync());
  if (m == null) fail('no se encontró la constante $nombre en $archivo');
  return int.parse(m.group(1)!);
}
