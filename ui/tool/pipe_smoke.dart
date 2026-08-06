// Drives the real pipe against a real daemon, from a process WITHOUT
// privileges. It is the only way to find out whether this half works: the
// widget suite cannot open a handle, and the daemon's own tests cannot tell
// whether Dart can talk to it.
//
//   dart run tool/pipe_smoke.dart                 lee, no toca nada
//   dart run tool/pipe_smoke.dart --sala          crea una sala y la cierra
//   dart run tool/pipe_smoke.dart --pipe <nombre>
//
// The daemon has to be up in an elevated console:
//   C:\kt\stage\kanpachid.exe --console -data C:\ProgramData\Kanpachi
//
// This process must NOT be elevated. That is the point: the protected prefix
// restricts who CREATES the name, and the daemon's descriptor lets the
// interactive user open it. Running this elevated would prove nothing.

import 'dart:io';

import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_client.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_methods.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/pipe_names.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/windows_pipe_transport.dart';

int fallos = 0;

/// Traza SÍNCRONA a disco, y no es lujo.
///
/// `stdout` en Dart va bufferizado cuando no es una terminal, así que si el
/// proceso se cae la última línea impresa NO es la última que se ejecutó, y
/// leer el crash como si lo fuera manda a buscar el fallo en el sitio
/// equivocado. Ya pasó una vez en esta misma herramienta.
final File _traza = File(
  '${Platform.environment['TEMP'] ?? '.'}\\pipe_smoke.trace',
);

void marca(String t) =>
    _traza.writeAsStringSync('$t\n', mode: FileMode.append, flush: true);

void paso(String t) {
  marca('PASO $t');
  stdout.writeln('\n=== $t ===');
}

void bien(String t) {
  marca('OK   $t');
  stdout.writeln('  OK  $t');
}

void nota(String t) {
  marca('--   $t');
  stdout.writeln('  --  $t');
}

void mal(String t) {
  fallos++;
  marca('MAL  $t');
  stdout.writeln('  MAL $t');
}

Future<void> main(List<String> args) async {
  final bool conSala = args.contains('--sala');
  final int iPipe = args.indexOf('--pipe');
  final String nombre = iPipe >= 0 && iPipe + 1 < args.length
      ? args[iPipe + 1]
      : PipeNames.console;

  if (_traza.existsSync()) _traza.deleteSync();

  paso('el token');
  final String? token = await readApiToken();
  if (token == null) {
    mal('no hay ${PipeNames.tokenPath}, o sea que el daemon no está arriba');
    exit(1);
  }
  bien('leído, ${token.length} caracteres');

  paso('abrir el pipe sin elevar');
  nota(nombre);
  final WindowsPipeTransport t = WindowsPipeTransport(name: nombre);
  final DaemonClient c = DaemonClient(transport: t, token: token);

  try {
    await c.connect();
    bien('conectado y saludado');
  } on DaemonUnreachable catch (e) {
    mal('${e.reason}  [${e.kind.name}]');
    exit(1);
  } on DaemonError catch (e) {
    mal('el daemon rechazó el saludo: $e');
    exit(1);
  }

  try {
    paso('status');
    final Map<String, Object?> st = await c.call(DaemonMethods.status);
    bien('conn=${st['conn']}  role=${st['role']}  code=${st['code'] ?? '-'}');

    paso('list_games, que es el que contesta con una lista');
    final List<Object?> juegos = await c.callList(DaemonMethods.listGames);
    if (juegos.isEmpty) {
      mal(
        'vino vacío: o falta builtin.json al lado del daemon, o el códec '
        'volvió a tirar los resultados que son listas',
      );
    } else {
      bien('${juegos.length} juegos');
      for (final Object? j in juegos.take(3)) {
        final Map<String, Object?> g = j! as Map<String, Object?>;
        nota('${g['id']}  ${g['name']}');
      }
    }

    paso('exposure');
    final Map<String, Object?> exp = await c.call(DaemonMethods.exposure);
    bien(
      'medido, ${(exp['rules'] as List<Object?>? ?? const <Object?>[]).length} reglas propias',
    );

    paso('un método que no existe');
    try {
      await c.call('inventado');
      mal('lo aceptó');
    } on DaemonError catch (e) {
      bien('rechazado con ${e.code}');
    }

    if (conSala) {
      await _laSala(c);
    } else {
      nota('sin --sala no se crea ninguna, que tarda como un minuto');
    }
  } on DaemonUnreachable catch (e) {
    mal('${e.reason}  [${e.kind.name}]');
  } on DaemonError catch (e) {
    mal('$e');
  } finally {
    await c.close();
    bien('cerrado');
  }

  paso('resultado');
  stdout.writeln(fallos == 0 ? '  todo verde' : '  $fallos fallo(s)');
  exit(fallos == 0 ? 0 : 1);
}

/// The slow half: raising a room really does take about a minute, which is the
/// whole reason the timeouts are per method.
Future<void> _laSala(DaemonClient c) async {
  paso('crear una sala, que tarda de verdad');
  final Stopwatch reloj = Stopwatch()..start();
  final Map<String, Object?> sala = await c.call(DaemonMethods.createRoom, {
    'nickname': 'Alvaro',
    'name': 'Prueba desde Dart',
  });
  reloj.stop();
  bien('creada en ${reloj.elapsed.inSeconds} s, código ${sala['code']}');
  nota(
    'conn=${sala['conn']}  ip=${sala['local_ip']}  subred=${sala['subnet']}',
  );

  paso('poner un juego');
  final Map<String, Object?> conJuego = await c.call(
    DaemonMethods.activateProfile,
    <String, Object?>{'game': 'project-zomboid'},
  );
  bien('juego=${conJuego['game']}  ${conJuego['game_name']}');

  paso('renombrar');
  final Map<String, Object?> renombrada = await c.call(
    DaemonMethods.renameRoom,
    <String, Object?>{'name': 'Los panas'},
  );
  bien('ahora se llama ${renombrada['name']}');

  paso('el enlace de invitación');
  final Map<String, Object?> enlace = await c.call(DaemonMethods.inviteLink);
  nota('${enlace['link']}');

  paso('salir');
  await c.call(DaemonMethods.leaveRoom);
  bien('fuera');
}
