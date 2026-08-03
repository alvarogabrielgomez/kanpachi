import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_client.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_codec.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_transport.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/status_mapper.dart';

/// El daemon falso: un transporte de memoria que habla el mismo contrato.
///
/// Existe porque el transporte real todavía no se puede escribir (`dart:io` no
/// habla named pipes de Windows, ver `daemon_transport.dart`) y porque, aunque
/// existiera, la lógica que estos tests cubren no es la del pipe: es el saludo,
/// la correlación por id, los plazos y el troceo. Eso se prueba mejor sobre
/// bytes en memoria, y así corre en cualquier máquina.
class _TransporteFalso implements DaemonTransport {
  final StreamController<List<int>> _entrada =
      StreamController<List<int>>.broadcast();

  /// Lo que el cliente escribió, ya decodificado.
  final List<Map<String, Object?>> enviado = <Map<String, Object?>>[];

  /// Si está puesto, contesta solo a estos métodos y calla al resto.
  Set<String>? contestaSolo;

  /// Rompe la escritura, para el camino de "no se pudo ni preguntar".
  bool escrituraRota = false;

  bool cerrado = false;

  @override
  Stream<List<int>> get incoming => _entrada.stream;

  @override
  Future<void> send(List<int> bytes) async {
    if (escrituraRota) throw Exception('el pipe se cayó');

    final Map<String, Object?> req =
        jsonDecode(utf8.decode(bytes).trim()) as Map<String, Object?>;
    enviado.add(req);

    final String metodo = req['method'] as String;
    if (contestaSolo != null && !contestaSolo!.contains(metodo)) return;

    responde(req['id'] as int, <String, Object?>{'ok': true});
  }

  void responde(int id, Map<String, Object?> result) {
    _entrada.add(utf8.encode(
      '${jsonEncode(<String, Object?>{'id': id, 'result': result})}\n',
    ));
  }

  void respondeConError(int id, String code, String message) {
    _entrada.add(utf8.encode(
      '${jsonEncode(<String, Object?>{
            'id': id,
            'error': <String, String>{'code': code, 'message': message},
          })}\n',
    ));
  }

  void crudo(String texto) => _entrada.add(utf8.encode(texto));

  Future<void> muere() => _entrada.close();

  @override
  Future<void> close() async {
    cerrado = true;
    if (!_entrada.isClosed) await _entrada.close();
  }
}

void main() {
  group('el saludo', () {
    test('va primero y lleva el token', () async {
      final _TransporteFalso t = _TransporteFalso();
      final DaemonClient c = DaemonClient(transport: t, token: 'secretísimo');

      await c.connect();

      expect(t.enviado.first['method'], equals('hello'));
      expect(
        (t.enviado.first['params']! as Map<String, Object?>)['token'],
        equals('secretísimo'),
        reason: 'sin token el daemon contesta unauthorized a todo',
      );
      await c.close();
    });

    test('sin saludar no sale ninguna petición', () async {
      final _TransporteFalso t = _TransporteFalso();
      final DaemonClient c = DaemonClient(transport: t, token: 'x');

      // Sin esto, la primera llamada de cada conexión fallaría con
      // unauthorized y el usuario vería un error que no es el suyo.
      await expectLater(
        c.call('status'),
        throwsA(isA<DaemonUnreachable>()),
      );
      expect(t.enviado, isEmpty);
      await c.close();
    });

    test('saludar dos veces no manda dos saludos', () async {
      final _TransporteFalso t = _TransporteFalso();
      final DaemonClient c = DaemonClient(transport: t, token: 'x');

      await c.connect();
      await c.connect();

      expect(t.enviado.length, equals(1));
      await c.close();
    });
  });

  group('las respuestas vuelven por id', () {
    test('dos peticiones a la vez no se cruzan', () async {
      final _TransporteFalso t = _TransporteFalso()
        ..contestaSolo = <String>{'hello'};
      final DaemonClient c = DaemonClient(transport: t, token: 'x');
      await c.connect();

      final Future<Map<String, Object?>> primera = c.call('status');
      final Future<Map<String, Object?>> segunda = c.call('list_games');

      // Se contestan AL REVÉS. Un cliente que empareje por orden de llegada
      // devolvería los juegos a quien preguntó por el estado, y es un fallo que
      // no aparece hasta que dos pantallas preguntan a la vez.
      final int idSegunda = t.enviado[2]['id']! as int;
      final int idPrimera = t.enviado[1]['id']! as int;
      t.responde(idSegunda, <String, Object?>{'quien': 'list_games'});
      t.responde(idPrimera, <String, Object?>{'quien': 'status'});

      expect((await primera)['quien'], equals('status'));
      expect((await segunda)['quien'], equals('list_games'));
      await c.close();
    });

    test('un error del daemon llega con su código, no como texto', () async {
      final _TransporteFalso t = _TransporteFalso()
        ..contestaSolo = <String>{'hello'};
      final DaemonClient c = DaemonClient(transport: t, token: 'x');
      await c.connect();

      final Future<Map<String, Object?>> f = c.call('kick_member');
      t.respondeConError(
        t.enviado[1]['id']! as int,
        'not_a_member',
        'esa dirección no es de nadie presente',
      );

      final DaemonError e = await f.then<DaemonError>(
        (_) => throw StateError('debió fallar'),
        onError: (Object e) => e as DaemonError,
      );
      expect(e.resolved, equals(FailureCode.notAMember));
      await c.close();
    });

    test('un código que esta versión no conoce no se pierde', () async {
      final _TransporteFalso t = _TransporteFalso()
        ..contestaSolo = <String>{'hello'};
      final DaemonClient c = DaemonClient(transport: t, token: 'x');
      await c.connect();

      final Future<Map<String, Object?>> f = c.call('status');
      t.respondeConError(t.enviado[1]['id']! as int, 'del_futuro', 'algo nuevo');

      final DaemonError e = await f.then<DaemonError>(
        (_) => throw StateError('debió fallar'),
        onError: (Object e) => e as DaemonError,
      );
      expect(e.resolved, isNull);
      expect(
        e.code,
        equals('del_futuro'),
        reason: 'el código crudo es la única pista útil que le queda al log',
      );
      await c.close();
    });
  });

  group('nada espera para siempre', () {
    test('una petición sin respuesta vence', () async {
      final _TransporteFalso t = _TransporteFalso()
        ..contestaSolo = <String>{'hello'};
      final DaemonClient c = DaemonClient(
        transport: t,
        token: 'x',
        timeout: const Duration(milliseconds: 80),
      );
      await c.connect();

      await expectLater(c.call('status'), throwsA(isA<DaemonUnreachable>()));
      await c.close();
    });

    test('si el daemon muere, las esperas se rompen todas juntas', () async {
      final _TransporteFalso t = _TransporteFalso()
        ..contestaSolo = <String>{'hello'};
      final DaemonClient c = DaemonClient(
        transport: t,
        token: 'x',
        // Largo a propósito: lo que rompe la espera tiene que ser la muerte del
        // daemon, no el plazo. Sin eso, la app deja las ruedas girando una a
        // una hasta que vencen.
        timeout: const Duration(seconds: 30),
      );
      await c.connect();

      final Future<Map<String, Object?>> a = c.call('status');
      final Future<Map<String, Object?>> b = c.call('list_games');
      await t.muere();

      await expectLater(a, throwsA(isA<DaemonUnreachable>()));
      await expectLater(b, throwsA(isA<DaemonUnreachable>()));
      await c.close();
    });

    test('no se puede escribir: falla como inalcanzable, no como error de la API',
        () async {
      final _TransporteFalso t = _TransporteFalso();
      final DaemonClient c = DaemonClient(transport: t, token: 'x');
      t.escrituraRota = true;

      await expectLater(c.connect(), throwsA(isA<DaemonUnreachable>()));
      await c.close();
    });
  });

  group('el troceo del flujo', () {
    test('una respuesta partida en dos lecturas se arma igual', () async {
      final _TransporteFalso t = _TransporteFalso()
        ..contestaSolo = <String>{'hello'};
      final DaemonClient c = DaemonClient(transport: t, token: 'x');
      await c.connect();

      final Future<Map<String, Object?>> f = c.call('status');
      final int id = t.enviado[1]['id']! as int;

      // Suponer que cada lectura es un mensaje entero es el error clásico de
      // quien enmarca sobre un flujo, y con un pipe pasa de verdad.
      t.crudo('{"id":$id,"result":{"conn"');
      t.crudo(':"connected"}}\n');

      expect((await f)['conn'], equals('connected'));
      await c.close();
    });

    test('dos respuestas en una sola lectura salen las dos', () async {
      final _TransporteFalso t = _TransporteFalso()
        ..contestaSolo = <String>{'hello'};
      final DaemonClient c = DaemonClient(transport: t, token: 'x');
      await c.connect();

      final Future<Map<String, Object?>> a = c.call('status');
      final Future<Map<String, Object?>> b = c.call('list_games');
      final int idA = t.enviado[1]['id']! as int;
      final int idB = t.enviado[2]['id']! as int;

      t.crudo('{"id":$idA,"result":{"q":"a"}}\n{"id":$idB,"result":{"q":"b"}}\n');

      expect((await a)['q'], equals('a'));
      expect((await b)['q'], equals('b'));
      await c.close();
    });
  });

  group('el códec por su cuenta', () {
    test('una línea por encima del tope corta la conversación', () {
      final DaemonCodec codec = DaemonCodec(maxBytes: 64);

      expect(
        () => codec.feed(utf8.encode('x' * 200)),
        throwsA(isA<DaemonProtocolError>()),
        reason: 'sin tope, un daemon que nunca cierra la línea se come la '
            'memoria de la app',
      );
      // Y no se reanuda: quien mandó una línea imposible ya perdió el hilo, y
      // seguir leyendo es interpretar media respuesta como si fuera una.
      expect(
        () => codec.feed(utf8.encode('{"id":1}\n')),
        throwsA(isA<DaemonProtocolError>()),
      );
    });

    test('una línea vacía no es un mensaje', () {
      final DaemonCodec codec = DaemonCodec();
      expect(codec.feed(utf8.encode('\n\n')), isEmpty);
    });

    test('una respuesta sin id no se puede emparejar con nada', () {
      final DaemonCodec codec = DaemonCodec();
      expect(
        () => codec.feed(utf8.encode('{"result":{}}\n')),
        throwsA(isA<DaemonProtocolError>()),
      );
    });
  });

  group('el estado que llega del daemon', () {
    test('las alertas salen resueltas a texto y en su orden', () {
      final DaemonStatus s = StatusMapper.fromJson(<String, Object?>{
        'conn': 'connected',
        'alerts': <Object?>[
          <String, Object?>{
            'kind': 'firewall_off',
            'detail': 'apagado en el perfil público',
          },
          <String, Object?>{'kind': 'audit_failed'},
        ],
      });

      expect(s.conn, equals(ConnState.connected));
      expect(s.alerts.length, equals(2));
      expect(s.alerts.first.title, contains('Firewall'));
      expect(
        s.alerts.first.detail,
        equals('apagado en el perfil público'),
        reason: 'el dato del daemon acompaña al copy del producto',
      );
      expect(s.alerts[1].title, contains('no pudo comprobar'));
    });

    test('una alerta que esta versión no conoce no se pierde', () {
      final DaemonStatus s = StatusMapper.fromJson(<String, Object?>{
        'conn': 'connected',
        'alerts': <Object?>[
          <String, Object?>{'kind': 'inventada', 'detail': 'lo que sea'},
        ],
      });

      expect(s.alerts.length, equals(1));
      expect(s.alerts.first.detail, equals('lo que sea'));
      expect(
        s.alerts.first.body,
        isNotEmpty,
        reason: 'perder el aviso es peor que perder el copy bueno',
      );
    });

    test('un estado desconocido cuenta como idle', () {
      // Es el único valor seguro: no promete que haya sala.
      final DaemonStatus s =
          StatusMapper.fromJson(<String, Object?>{'conn': 'teletransportando'});
      expect(s.conn, equals(ConnState.idle));
    });

    test('sin alertas no es un error, es el caso normal', () {
      expect(StatusMapper.fromJson(<String, Object?>{}).hasAlerts, isFalse);
      expect(
        StatusMapper.fromJson(<String, Object?>{'alerts': 'basura'}).alerts,
        isEmpty,
      );
      expect(
        StatusMapper.fromJson(<String, Object?>{
          'alerts': <Object?>[42, null, <String, Object?>{'sin': 'kind'}],
        }).alerts,
        isEmpty,
        reason: 'un elemento sin kind no es una alerta a medias: no es una '
            'alerta',
      );
    });

    test('el motivo de salida sale resuelto, y salir por tu cuenta no dice nada',
        () {
      expect(
        StatusMapper.fromJson(<String, Object?>{'last_exit': 'kicked'})
            .lastExit!
            .body,
        contains('te sacó'),
      );
      expect(
        StatusMapper.fromJson(<String, Object?>{'last_exit': 'user'}).lastExit,
        isNull,
      );
      expect(
        StatusMapper.fromJson(<String, Object?>{}).lastExit,
        isNull,
      );
    });
  });
}
