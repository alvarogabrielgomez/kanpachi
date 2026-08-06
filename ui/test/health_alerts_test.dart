import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/features/home/presentation/pages/home_page.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/room_page.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_methods.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

import 'daemon_client_test.dart' show DaemonTestFailure, daemonTestConnector;

/// Los avisos de salud de la portada salen del daemon y de ningún otro sitio.
///
/// # Qué se afirma acá, y por qué hace falta afirmarlo
///
/// Esta sección vivió con una lista escrita a mano, con `firewallOff` y
/// `routerMapping` fijas, mientras el daemon no existía. Una maqueta así no
/// falla nunca: enseña dos avisos verosímiles con la máquina sana y sigue
/// enseñándolos con la máquina rota. El día que alguien la reponga por comodidad
/// (para ver cómo queda, para una captura), la pantalla vuelve a mentir en
/// silencio y ningún test se entera.
///
/// De ahí la forma de estas afirmaciones: **con el daemon callado no se pinta
/// NADA**. Es lo único que una lista literal no puede pasar.
void main() {
  Future<SessionCubit> pintaPortada(
    WidgetTester tester,
    Map<String, Object?> status,
  ) async {
    final _DaemonScenario daemon = _DaemonScenario(status);
    final SessionCubit session = SessionCubit(
      PipeSessionRepository(daemonTestConnector(daemon.respond)),
    );
    addTearDown(session.close);
    await session.refreshHealth();

    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.light(),
        home: MultiBlocProvider(
          providers: <BlocProvider<dynamic>>[
            BlocProvider<ShellCubit>(create: (_) => ShellCubit()),
            BlocProvider<SessionCubit>.value(value: session),
          ],
          child: const Scaffold(body: HomeScreen()),
        ),
      ),
    );
    await tester.pumpAndSettle();
    return session;
  }

  const String tituloFirewall = 'Tu Firewall de Windows está apagado';
  const String tituloRouter =
      'Tu router tiene un puerto abierto hacia internet';

  // LA AFIRMACIÓN QUE MATA LA MAQUETA.
  //
  // Con el daemon sin avisos, la portada no pinta ni uno. Volver a poner una
  // lista literal hace fallar esto y nada más lo haría fallar.
  testWidgets('sin avisos del daemon la portada no pinta ninguno', (
    WidgetTester tester,
  ) async {
    await pintaPortada(tester, <String, Object?>{});

    expect(find.text(tituloFirewall), findsNothing);
    expect(
      find.text(tituloRouter),
      findsNothing,
      reason:
          'estas dos eran las de la maqueta: si aparecen sin que el daemon '
          'las mande, la lista literal volvió',
    );
  });

  testWidgets('se pinta lo que mandó el daemon y solo eso', (
    WidgetTester tester,
  ) async {
    await pintaPortada(tester, <String, Object?>{
      'alerts': <Object?>[
        <String, Object?>{'kind': 'router_mapping'},
      ],
    });

    expect(find.text(tituloRouter), findsOneWidget);
    expect(find.text(tituloFirewall), findsNothing);
  });

  // El detalle es del daemon y el copy es del producto. La maqueta lo tiraba:
  // llamaba a `alert(kind)` sin detalle, así que el dato medido no llegaba a la
  // pantalla ni cuando existía.
  testWidgets('el detalle del daemon viaja hasta la pantalla', (
    WidgetTester tester,
  ) async {
    await pintaPortada(tester, <String, Object?>{
      'alerts': <Object?>[
        <String, Object?>{
          'kind': 'router_mapping',
          'detail': 'TCP 25565 hacia 192.168.1.40',
        },
      ],
    });

    expect(find.textContaining('TCP 25565 hacia 192.168.1.40'), findsOneWidget);
  });

  // UN AVISO NO SE PIERDE POR SER MÁS NUEVO QUE LA UI.
  //
  // El daemon y la app se actualizan por separado. Una clave que este enum no
  // tiene va a existir tarde o temprano, y la respuesta correcta jamás es
  // callarla: se pierde el copy bueno y se conserva el aviso.
  testWidgets('un aviso que esta versión no conoce se pinta igual', (
    WidgetTester tester,
  ) async {
    await pintaPortada(tester, <String, Object?>{
      'alerts': <Object?>[
        <String, Object?>{
          'kind': 'algo_que_no_existe_todavia',
          'detail': 'puerto 9999',
        },
      ],
    });

    expect(
      find.textContaining('esta versión de la app no sabe explicar'),
      findsOneWidget,
    );
    expect(find.textContaining('puerto 9999'), findsOneWidget);
  });

  group('la banda de la sala', () {
    Future<_DaemonScenario> pintaSala(
      WidgetTester tester,
      Map<String, Object?> health,
    ) async {
      final _DaemonScenario daemon = _DaemonScenario(<String, Object?>{
        ..._roomStatus,
        ...health,
      });
      final SessionCubit session = SessionCubit(
        PipeSessionRepository(daemonTestConnector(daemon.respond)),
      );
      addTearDown(session.close);
      await session.refresh();

      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: MultiBlocProvider(
            providers: <BlocProvider<dynamic>>[
              BlocProvider<ShellCubit>(create: (_) => ShellCubit()),
              BlocProvider<SessionCubit>.value(value: session),
            ],
            child: const Scaffold(body: RoomScreen()),
          ),
        ),
      );
      await tester.pumpAndSettle();
      return daemon;
    }

    final Map<String, Object?> conFuga = <String, Object?>{
      'alerts': <Object?>[
        <String, Object?>{'kind': 'canary_leaking'},
      ],
      'canary': <String, Object?>{
        'measured': true,
        'verdict': 'leaking',
        'port': 51234,
        'touched': true,
        'asked': <Object?>['Gabriel', 'Santiago'],
      },
    };

    // LA SALA TIENE QUE ENSEÑARLA SIN QUE NADIE LA BUSQUE.
    //
    // El usuario puede no abrir jamás la pantalla de exposición. Si la alarma
    // viviera solo allí, una protección que dejó de contener se quedaría sin
    // ver hasta que a alguien se le ocurriera ir a mirar.
    testWidgets('con la alarma puesta la sala la enseña sola', (
      WidgetTester tester,
    ) async {
      await pintaSala(tester, conFuga);

      expect(find.text('Tu protección no está conteniendo'), findsOneWidget);
      // El puerto NO se nombra, por la regla de docs/05-ui.md: es efimero y ya
      // esta cerrado. Lo que va es quien lo comprobo.
      expect(find.textContaining('51234'), findsNothing);
      expect(find.textContaining('2 personas de la sala'), findsOneWidget);
    });

    testWidgets('sin la alarma la sala no la enseña', (
      WidgetTester tester,
    ) async {
      await pintaSala(tester, <String, Object?>{});

      expect(find.text('Tu protección no está conteniendo'), findsNothing);
    });

    // EL BOTÓN LLEGA AL DAEMON.
    //
    // Es lo único que el test del widget no puede afirmar: allí el callback es
    // de mentira. Acá se comprueba que la pantalla lo cableó al cubit y que el
    // cubit llama al repositorio.
    testWidgets('pulsar reponer llega hasta el daemon', (
      WidgetTester tester,
    ) async {
      final _DaemonScenario daemon = await pintaSala(tester, conFuga);

      await tester.tap(find.text('Volver a aplicar la protección'));
      await tester.pumpAndSettle();

      expect(daemon.reapplications, equals(1));
      expect(
        find.text('Tu protección no está conteniendo'),
        findsNothing,
        reason:
            'el daemon contestó sin la alerta, así que la banda tiene que '
            'irse sola: dejarla puesta obligaría al usuario a adivinar si su '
            'pulsación sirvió de algo',
      );
    });

    testWidgets('un código perdido se renueva sin cortar la sala', (
      WidgetTester tester,
    ) async {
      final _DaemonScenario daemon = await pintaSala(tester, <String, Object?>{
        'code_lost': true,
      });

      expect(
        find.text('El código de esta sala dejó de funcionar'),
        findsOneWidget,
      );

      await tester.tap(find.text('Renovar el código'));
      await tester.pumpAndSettle();

      expect(daemon.codeRenewals, equals(1));
      expect(
        find.text('El código de esta sala dejó de funcionar'),
        findsNothing,
      );
      expect(find.text('B8L3-N4PZ'), findsOneWidget);
    });
  });

  group('el cubit', () {
    test('reponer guarda lo que contestó el daemon', () async {
      final _DaemonScenario daemon = _DaemonScenario(<String, Object?>{
        'alerts': <Object?>[
          <String, Object?>{'kind': 'canary_leaking'},
        ],
        'canary': <String, Object?>{'measured': true, 'verdict': 'leaking'},
      });
      final SessionCubit session = SessionCubit(
        PipeSessionRepository(daemonTestConnector(daemon.respond)),
      );
      addTearDown(session.close);
      await session.refreshHealth();
      expect(session.state.health.alerts.single.wire, equals('canary_leaking'));

      await session.reapplyProtection();

      expect(daemon.reapplications, equals(1));
      expect(
        session.state.health.alerts,
        isEmpty,
        reason:
            'la pantalla tiene que redibujar sin la alerta que se acaba de '
            'resolver, y para eso el cubit guarda lo que contestó el daemon en '
            'vez de volver a preguntar',
      );
      expect(session.state.isReapplying, isFalse);
    });

    // El botón que escribe en el firewall se apaga con esta bandera. Sin el
    // `finally`, un fallo lo dejaría apagado para siempre y el usuario se
    // quedaría con la alarma puesta y sin forma de reintentar.
    test('un fallo al reponer no deja el botón apagado', () async {
      final _DaemonScenario daemon = _DaemonScenario(
        <String, Object?>{},
        failReapply: true,
      );
      final SessionCubit session = SessionCubit(
        PipeSessionRepository(daemonTestConnector(daemon.respond)),
      );
      addTearDown(session.close);

      await session.reapplyProtection();

      expect(session.state.isReapplying, isFalse);
      expect(session.state.failure, isNotNull);
    });
  });
}

const Map<String, Object?> _roomStatus = <String, Object?>{
  'conn': 'connected',
  'role': 'host',
  'name': 'La Guarida',
  'code': 'A7K2-M9QX',
  'host_present': true,
  'peers': <Object?>[
    <String, Object?>{
      'ip': '100.87.3.1',
      'name': 'Alvaro',
      'path': 'self',
      'self': true,
      'host': true,
    },
  ],
};

/// Mutable daemon-side facts; the repository and cubit above it remain real.
final class _DaemonScenario {
  _DaemonScenario(this.status, {this.failReapply = false});

  Map<String, Object?> status;
  final bool failReapply;
  int reapplications = 0;
  int codeRenewals = 0;

  Object? respond(String method, Map<String, Object?>? _) {
    switch (method) {
      case DaemonMethods.status:
        return status;
      case DaemonMethods.reapplyProtection:
        if (failReapply) {
          return const DaemonTestFailure('internal', 'el daemon dijo que no');
        }
        reapplications++;
        status = <String, Object?>{
          ...status,
          'alerts': <Object?>[],
          'canary': <String, Object?>{'measured': false},
        };
        return status;
      case DaemonMethods.rotateInviteCode:
        codeRenewals++;
        status = <String, Object?>{
          ...status,
          'code': 'B8L3-N4PZ',
          'code_lost': false,
        };
        return status;
      case DaemonMethods.listGames:
      case DaemonMethods.foreignRules:
        return <Object?>[];
      default:
        return <String, Object?>{};
    }
  }
}
