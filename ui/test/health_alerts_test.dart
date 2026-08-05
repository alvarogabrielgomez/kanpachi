import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/theme/app_theme.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/home/presentation/pages/home_page.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/room_page.dart';
import 'package:kanpachi_ui/features/session/domain/entities/canary.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/infra/fake_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

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
    HealthReport salud,
  ) async {
    final SessionCubit session = SessionCubit(RepoConSalud(salud));
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
    await pintaPortada(tester, const HealthReport.unknown());

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
    await pintaPortada(
      tester,
      const HealthReport(
        alerts: <HealthAlert>[
          HealthAlert(wire: 'router_mapping', kind: AlertKind.routerMapping),
        ],
      ),
    );

    expect(find.text(tituloRouter), findsOneWidget);
    expect(find.text(tituloFirewall), findsNothing);
  });

  // El detalle es del daemon y el copy es del producto. La maqueta lo tiraba:
  // llamaba a `alert(kind)` sin detalle, así que el dato medido no llegaba a la
  // pantalla ni cuando existía.
  testWidgets('el detalle del daemon viaja hasta la pantalla', (
    WidgetTester tester,
  ) async {
    await pintaPortada(
      tester,
      const HealthReport(
        alerts: <HealthAlert>[
          HealthAlert(
            wire: 'router_mapping',
            kind: AlertKind.routerMapping,
            detail: 'TCP 25565 hacia 192.168.1.40',
          ),
        ],
      ),
    );

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
    await pintaPortada(
      tester,
      const HealthReport(
        alerts: <HealthAlert>[
          HealthAlert(
            wire: 'algo_que_no_existe_todavia',
            detail: 'puerto 9999',
          ),
        ],
      ),
    );

    expect(
      find.textContaining('esta versión de la app no sabe explicar'),
      findsOneWidget,
    );
    expect(find.textContaining('puerto 9999'), findsOneWidget);
  });

  group('la banda de la sala', () {
    /// La sala se pide una sola vez: el repositorio falso simula la espera real
    /// de crearla y pagarla en cada test multiplicaría lo que tarda la suite.
    late final Room sala;

    setUpAll(() async {
      sala = await FakeSessionRepository().createRoom(name: 'La Guarida');
    });

    Future<RepoConSalud> pintaSala(
      WidgetTester tester,
      HealthReport salud,
    ) async {
      final RepoConSalud repo = RepoConSalud(salud);
      final SessionCubit session = SessionCubit(repo)..debugReplaceRoom(sala);
      await session.refreshHealth();

      await tester.pumpWidget(
        MaterialApp(
          theme: AppTheme.light(),
          home: MultiBlocProvider(
            providers: <BlocProvider<dynamic>>[
              BlocProvider<ShellCubit>(create: (_) => ShellCubit()),
              BlocProvider<SessionCubit>.value(value: session),
            ],
            child: const Scaffold(
              body: SingleChildScrollView(child: RoomScreen()),
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();
      return repo;
    }

    const HealthReport conFuga = HealthReport(
      alerts: <HealthAlert>[
        HealthAlert(wire: 'canary_leaking', kind: AlertKind.canaryLeaking),
      ],
      canary: CanaryCheck(
        measured: true,
        verdict: CanaryVerdict.leaking,
        port: 51234,
        touched: true,
        asked: <String>['Gabriel', 'Santiago'],
      ),
    );

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
      await pintaSala(tester, const HealthReport.unknown());

      expect(find.text('Tu protección no está conteniendo'), findsNothing);
    });

    // EL BOTÓN LLEGA AL DAEMON.
    //
    // Es lo único que el test del widget no puede afirmar: allí el callback es
    // de mentira. Acá se comprueba que la pantalla lo cableó al cubit y que el
    // cubit llama al repositorio.
    testWidgets('pulsar reponer llega hasta el repositorio', (
      WidgetTester tester,
    ) async {
      final RepoConSalud repo = await pintaSala(tester, conFuga);

      await tester.tap(find.text('Volver a aplicar la protección'));
      await tester.pumpAndSettle();

      expect(repo.reposiciones, equals(1));
      expect(
        find.text('Tu protección no está conteniendo'),
        findsNothing,
        reason:
            'el daemon contestó sin la alerta, así que la banda tiene que '
            'irse sola: dejarla puesta obligaría al usuario a adivinar si su '
            'pulsación sirvió de algo',
      );
    });
  });

  group('el cubit', () {
    test('reponer guarda lo que contestó el daemon', () async {
      final RepoConSalud repo = RepoConSalud(
        const HealthReport(
          alerts: <HealthAlert>[
            HealthAlert(wire: 'canary_leaking', kind: AlertKind.canaryLeaking),
          ],
          canary: CanaryCheck(measured: true, verdict: CanaryVerdict.leaking),
        ),
      );
      final SessionCubit session = SessionCubit(repo);
      await session.refreshHealth();
      expect(session.state.health.kinds, contains(AlertKind.canaryLeaking));

      await session.reapplyProtection();

      expect(repo.reposiciones, equals(1));
      expect(
        session.state.health.kinds,
        isNot(contains(AlertKind.canaryLeaking)),
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
      final SessionCubit session = SessionCubit(_RepoQueFalla());

      await expectLater(
        session.reapplyProtection(),
        throwsA(isA<StateError>()),
      );

      expect(session.state.isReapplying, isFalse);
    });
  });
}

/// El repositorio falso con la salud que pida cada test.
///
/// Hereda del falso de verdad en vez de escribir un doble a mano, y eso no es
/// pereza: así el día que la interfaz crezca, este test deja de compilar en vez
/// de seguir verde probando una pantalla que ya no existe.
final class RepoConSalud extends FakeSessionRepository {
  RepoConSalud(this.salud);

  final HealthReport salud;

  int reposiciones = 0;

  @override
  Future<HealthReport> health() async => salud;

  @override
  Future<HealthReport> reapplyProtection() async {
    reposiciones++;
    return const HealthReport();
  }
}

final class _RepoQueFalla extends FakeSessionRepository {
  @override
  Future<HealthReport> reapplyProtection() async =>
      throw StateError('el daemon dijo que no');
}
