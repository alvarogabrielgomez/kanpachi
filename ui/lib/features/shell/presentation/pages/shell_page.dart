import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/action_failure.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/failure_notice.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/messages/loading_phrases.dart';
import 'package:kanpachi_ui/features/shell/presentation/failure_navigation.dart';
import 'package:kanpachi_ui/features/games/presentation/pages/game_picker_page.dart';
import 'package:kanpachi_ui/features/games/presentation/pages/manual_game_page.dart';
import 'package:kanpachi_ui/features/home/presentation/pages/home_page.dart';
import 'package:kanpachi_ui/features/invite/presentation/pages/invite_pages.dart';
import 'package:kanpachi_ui/features/onboarding/presentation/pages/onboarding_pages.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/exposure_page.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/room_page.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/room_dialogs.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/pages/loading_page.dart';
import 'package:kanpachi_ui/features/seed/presentation/pages/seed_password_screen.dart';
import 'package:kanpachi_ui/features/seed/presentation/pages/seed_screen.dart';
import 'package:kanpachi_ui/features/seed/presentation/widgets/trust_seed_dialog.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/settings/presentation/pages/settings_page.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/shell_bars.dart';

/// El marco de la aplicación: la ventana ES la app.
///
/// El archivo de diseño enseña la app como una captura — un lienzo con manchas
/// de color y la ventana flotando encima con su sombra. Eso es cómo se
/// PRESENTA un mockup de escritorio, no cómo se construye: aquí la barra de
/// título es el chrome de la ventana de verdad, y el contenido llega hasta los
/// bordes. Dibujar una ventana dentro de la ventana daría dos marcos, dos
/// juegos de botones y dos respuestas a la misma cruz.
class ShellPage extends StatelessWidget {
  const ShellPage({super.key});

  /// # Por qué acá no hay ni un `watch`, y sí tres `select`
  ///
  /// `watch` reconstruye con CUALQUIER cambio del estado, y el estado de la
  /// sesión cambia cada dos segundos porque el latido lo reafirma. Con un
  /// `watch` aquí, la barra de título entera —el nombre, los tres botones de
  /// ventana, sus tooltips— se reconstruía treinta veces por minuto para
  /// pintar exactamente lo mismo, y arrastraba con ella al cuerpo.
  ///
  /// Con `select` cada trozo se suscribe al VALOR que dibuja, y una barra que
  /// dice lo mismo no se toca. Es lo que hace que pulsar un botón de ventana
  /// responda cuando responde y no cuando le toca.
  @override
  Widget build(BuildContext context) {
    final String derecha = context.select<SessionCubit, String>(
      (SessionCubit c) => _statusRight(c.state),
    );
    final bool daemonCaido = context.select<SessionCubit, bool>(
      (SessionCubit c) => c.state.daemonDown,
    );
    // Sin sala, lo de la derecha ES el servidor, y por eso se puede pulsar. Va
    // como `select` propio y no deducido del texto: mirar si la cadena lleva un
    // punto sería adivinar desde el resultado lo que el estado ya sabe.
    final bool sinSala = context.select<SessionCubit, bool>(
      (SessionCubit c) => c.state.room == null,
    );

    return Scaffold(
      backgroundColor: context.colors.surface,
      body: Column(
        children: <Widget>[
          const ShellTitleBar(),
          const Expanded(child: _WindowBody()),
          ShellStatusBar(
            right: derecha,
            rightIsSeed: sinSala,
            daemonDown: daemonCaido,
          ),
        ],
      ),
    );
  }

  /// Cuando hay sala, el dato útil es el adaptador y tu IP dentro de ella;
  /// cuando no, a qué seed estás apuntando. Nunca los dos: la barra es una
  /// línea, y llenarla de datos la convierte en ruido que nadie lee.
  static String _statusRight(SessionState session) {
    final Room? room = session.room;
    // Sin sala, el registro CONFIGURADO. Acá había un dominio escrito a mano,
    // el del seed que venía de fábrica, y desde que no hay ninguno de fábrica
    // era una línea que decía lo mismo apuntara esta máquina donde apuntara.
    // Vacío significa que nadie eligió uno todavía, y eso también se dice.
    if (room == null) {
      return session.ownSeed.isEmpty ? 'sin servidor' : session.ownSeed;
    }
    final Member self = room.members.firstWhere(
      (Member m) => m.isSelf,
      orElse: () => room.members.first,
    );
    return 'kanpachi0 · ${self.address}';
  }
}

/// El área que cambia, con los diálogos por encima.
class _WindowBody extends StatelessWidget {
  const _WindowBody();

  @override
  Widget build(BuildContext context) {
    // Sin `watch` de nada: cada capa se suscribe a lo suyo.
    //
    // La pantalla activa ya se salvaba sola, y conviene saber por qué para no
    // "arreglarla" de nuevo: [_CurrentScreen] devuelve widgets `const`, así que
    // aunque este build corra, `Element.updateChild` ve el MISMO widget y se
    // salta el subárbol entero. Lo que no se salvaba era la capa de diálogos,
    // que recibía los dos estados por parámetro y por tanto era un widget nuevo
    // en cada emisión.

    // `fit: expand` y no un `Positioned.fill` suelto. Un Stack se mide por sus
    // hijos NO posicionados, y cuando no hay diálogo la capa de diálogos es un
    // `SizedBox.shrink()`: con todo lo demás posicionado, el Stack medía cero
    // de ancho y la ventana salía en blanco. Con diálogo abierto no pasaba
    // — el modal es `Positioned.fill`, no quedaba ningún hijo sin posicionar
    // y el Stack se iba a `constraints.biggest` — que es justo la clase de
    // bug que aparece en una pantalla y no en la de al lado.
    return Stack(
      fit: StackFit.expand,
      children: <Widget>[
        const _RoomFollower(child: _CurrentScreen()),
        // The failure notice sits ABOVE the screen and below the dialogs.
        //
        // Here and not inside each screen, because every screen has buttons
        // that can fail and the alternative is the same notice pasted into
        // seven places, six of which get it right. Anchored to the bottom so
        // it does not push the layout around: an action that fails must not
        // move the button the user is about to press again.
        const _FailureLayer(),
        // Dentro del marco y no como ruta aparte: los tres diálogos confirman
        // algo que cambia la sala que se ve por detrás, y dejarla visible tras
        // el velo es lo que da contexto a qué se está confirmando.
        const _DialogLayer(),
      ],
    );
  }
}

/// Lleva la pantalla a donde diga la sala DE VERDAD, la que tiene el daemon.
///
/// # El agujero que tapa
///
/// La navegación era de ida y nada más: se entraba a la sala porque el usuario
/// pulsó crear, y desde ahí la pantalla y la sala solo coincidían por
/// costumbre. Crear una sala, ir a Configuración y volver dejaba la app en la
/// portada con el daemon dentro de una sala, y crear otra contestaba `busy`:
/// una app bloqueada sin nada que la desbloqueara.
///
/// Ahora el latido trae la sala de verdad y esto la sigue. Los dos sentidos
/// hacen falta y son distintos:
///
///  - **Aparece una sala** y estamos en una pantalla de "sin sala": se va a la
///    sala. Cubre volver de Configuración, cubre reanudar la sala del arranque
///    anterior, y cubre que la sala la haya abierto otra cosa.
///  - **Desaparece la sala** y estamos en una pantalla que la necesita: se va a
///    la portada. Es lo que ya hacía el suelo de [_CurrentScreen], que se queda
///    igualmente como red de seguridad para el primer fotograma.
///
/// **No arrastra al usuario fuera de donde eligió estar.** Configuración, el
/// selector de juego y la exposición se ven con sala y sin ella, así que
/// aparecer una sala mientras alguien mira los ajustes no lo saca de ahí.
class _RoomFollower extends StatelessWidget {
  const _RoomFollower({required this.child});

  final Widget child;

  /// Las pantallas desde las que SÍ se salta al aparecer una sala.
  ///
  /// Lista corta y explícita en vez de "todas menos algunas": una pantalla
  /// nueva no debería empezar arrastrando a nadie por omisión.
  static const Set<AppScreen> _sinSala = <AppScreen>{
    AppScreen.home,
    AppScreen.welcome,
  };

  @override
  Widget build(BuildContext context) {
    return MultiBlocListener(
      listeners: <BlocListener<SessionCubit, SessionState>>[
        BlocListener<SessionCubit, SessionState>(
          listenWhen: (SessionState a, SessionState b) =>
              (a.room == null) != (b.room == null),
          listener: (BuildContext context, SessionState state) {
            final ShellCubit shell = context.read<ShellCubit>();
            final AppScreen ahora = shell.state.screen;
            // Acá se preguntaba por una versión nueva, al abrir y al cerrar
            // sala, y ya no: la comprobación pasó a ser a pedido. Ver
            // [UpdateCubit] para el motivo, que es de a QUIÉN se le pregunta y
            // no de cuándo.
            if (state.room != null) {
              if (_sinSala.contains(ahora)) shell.go(AppScreen.room);
              return;
            }
            if (ahora == AppScreen.room || ahora == AppScreen.exposure) {
              shell.leftRoom();
            }
          },
        ),
        // **Un registro que pide contraseña lleva a escribirla, y no a un
        // aviso que hay que interpretar.**
        //
        // Es el único fallo de la lista que se arregla escribiendo algo, así
        // que dejarlo como mensaje en la portada obligaría a leerlo, deducir
        // qué hacer y buscar dónde. Lleva a la pantalla, y el mismo código
        // cubre los tres casos: nunca se puso ninguna, caducó, o la cambiaron.
        //
        // Se dispara con el fallo APARECIENDO y no con cada cambio de estado:
        // sin eso, cerrar el aviso volvería a navegar.
        BlocListener<SessionCubit, SessionState>(
          listenWhen: (SessionState a, SessionState b) =>
              a.failure?.code != b.failure?.code &&
              screenForFailure(b.failure?.code) != null,
          listener: (BuildContext context, SessionState state) {
            final AppScreen? destino = screenForFailure(state.failure?.code);
            if (destino != null) context.read<ShellCubit>().go(destino);
          },
        ),
        // **Un enlace que llega manda, esté donde esté el usuario.**
        //
        // A diferencia de la sala, esto SÍ arrastra desde cualquier pantalla, y
        // la asimetría tiene motivo: la sala aparece por cosas que pasan solas
        // —alguien entró, el daemon reanudó— y el enlace es un acto deliberado
        // de la persona que está delante, que acaba de pulsar un botón en su
        // navegador y espera ver algo.
        BlocListener<SessionCubit, SessionState>(
          listenWhen: (SessionState a, SessionState b) =>
              (a.invite == null) != (b.invite == null),
          listener: (BuildContext context, SessionState state) {
            final ShellCubit shell = context.read<ShellCubit>();
            if (state.invite != null) {
              shell.go(AppScreen.invite);
              return;
            }
            // Se fue el enlace: se vuelve de donde se vino, que es lo que ya
            // sabe el historial.
            //
            // **Sin excepción por tener sala, que era la que dejaba la pantalla
            // clavada en `invite`.** Decía que con sala abierta mandaba el otro
            // oyente, y no es verdad: ese solo se despierta cuando la sala
            // aparece o desaparece, y acá la sala no se ha movido. Nadie
            // navegaba, la pantalla seguía siendo `invite` sin enlace que
            // pintar, y lo que se veía era la portada.
            if (shell.state.screen == AppScreen.invite) {
              shell.back();
            }
          },
        ),
        // **La sala del arranque anterior pregunta, no navega.**
        //
        // Es un diálogo y no una pantalla porque son dos botones y una frase, y
        // porque puede aparecer encima de donde sea. Y es el único diálogo que
        // se abre sin que nadie pulse nada: lo descubre el latido.
        //
        // `listenWhen` mira el borde, así que cerrar el diálogo sin elegir NO
        // lo vuelve a abrir a los dos segundos: el dato sigue ahí —preguntar no
        // lo consume— pero ya no cambia. Vuelve a ofrecerse al siguiente
        // arranque, que es cuando la pregunta vuelve a tener sentido.
        BlocListener<SessionCubit, SessionState>(
          listenWhen: (SessionState a, SessionState b) =>
              (a.pendingRoom == null) != (b.pendingRoom == null),
          listener: (BuildContext context, SessionState state) {
            final ShellCubit shell = context.read<ShellCubit>();
            if (state.pendingRoom != null) {
              shell.showDialog(AppDialog.resumeRoom);
              return;
            }
            // Se resolvió, por el botón que sea. El diálogo se cierra solo al
            // pulsar, y esto cubre el caso de que lo resuelva otra ventana.
            if (shell.state.dialog == AppDialog.resumeRoom) {
              shell.closeDialog();
            }
          },
        ),
      ],
      child: child,
    );
  }
}

/// The screen actually on show, or null while an operation is being waited on.
///
/// Shared by [_CurrentScreen] and [_FailureLayer] so the floor that sends a
/// roomless app back home is written once. Two copies of it would drift, and the
/// symptom would be the failure notice painted twice or not at all.
AppScreen? _visibleScreen(ShellState shell, SessionState session) {
  if (session.isWaiting) return null;
  // La confirmación de un enlace vive SIN sala, que es justo lo que el suelo de
  // abajo derriba. Se comprueba primero, y por eso: la pantalla de invitación
  // existe precisamente cuando todavía no se ha entrado a ninguna parte.
  //
  // **Y cuando el enlace se va, la caída es a la SALA si hay sala.** Caer
  // siempre a la portada era el agujero por el que un host acababa mirando una
  // portada con su sala abierta detrás: la pantalla seguía siendo `invite`, así
  // que el suelo de abajo —que solo mira `home` y `welcome`— no llegaba a
  // correr nunca. Medido en v0.1.3 con la sala arriba y `kanpachi0 · 10.99.9.1`
  // en la barra de estado, que es la propia app diciendo que sí había sala.
  if (shell.screen == AppScreen.invite) {
    if (session.invite != null) return AppScreen.invite;
    return session.room != null ? AppScreen.room : AppScreen.home;
  }
  // **Con sala abierta, la portada no es un sitio donde se pueda estar.**
  //
  // La portada ofrece crear una sala y entrar a otra, y las dos contestan
  // `busy` mientras haya una: es una pantalla entera cuyos botones no hacen
  // nada, con la sala de quien la mira escondida detrás. Se llegaba a ella por
  // varios caminos —una flecha de volver mal apuntada, cancelar un enlace, un
  // «Cerrar la sala» que falló— y cada uno se arreglaba por su lado.
  //
  // Esto lo cierra en el único sitio que decide qué se ve: mientras el daemon
  // diga que hay sala, la pantalla de la sala gana. Se comprueba DESPUÉS del
  // enlace, que sí se ve con sala abierta y a propósito.
  if (session.room != null &&
      (shell.screen == AppScreen.home || shell.screen == AppScreen.welcome)) {
    return AppScreen.room;
  }
  if (session.room == null &&
      (shell.screen == AppScreen.room || shell.screen == AppScreen.exposure)) {
    return AppScreen.home;
  }
  return shell.screen;
}

/// The notice for whatever the user asked for last and did not happen.
class _FailureLayer extends StatelessWidget {
  const _FailureLayer();

  /// The screens that paint the failure THEMSELVES, inside their left panel.
  ///
  /// They have somewhere to put it: a column of its own that scrolls, where the
  /// notice can be read whole and its details unfolded without covering the
  /// list next to it. The rest have no panel, so for them the frame keeps
  /// floating it at the bottom.
  static const Set<AppScreen> _withPanels = <AppScreen>{
    AppScreen.home,
    AppScreen.room,
  };

  @override
  Widget build(BuildContext context) {
    final ShellState shell = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;
    final ActionFailure? failure = session.failure;
    if (failure == null) return const SizedBox.shrink();
    // Un fallo que ya llevó a su pantalla no se cuenta además como aviso: el
    // aviso diría «pide contraseña» flotando sobre la pantalla que la pide.
    if (screenForFailure(failure.code) != null) {
      return const SizedBox.shrink();
    }
    if (_withPanels.contains(_visibleScreen(shell, session))) {
      return const SizedBox.shrink();
    }

    return Positioned(
      left: AppSpacing.x4l,
      right: AppSpacing.x4l,
      bottom: AppSpacing.x4l,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 720),
        child: FailureNotice(
          failure: failure,
          verbose: session.verbose,
          onDismiss: () => context.read<SessionCubit>().clearFailure(),
        ),
      ),
    );
  }
}

/// La pantalla activa.
///
/// Es una clase y no un método que devuelva Widget, aunque sea un `switch` de
/// una línea. La regla no admite excepciones "porque es corto": un helper cae
/// siempre en la rama `update()` de `Element.updateChild`, así que cada
/// `setState` del marco reconstruiría la pantalla entera en vez de saltársela
/// cuando no ha cambiado.
class _CurrentScreen extends StatelessWidget {
  const _CurrentScreen();

  @override
  Widget build(BuildContext context) {
    final ShellState shell = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;

    // Las esperas ganan a la pantalla elegida: mientras se crea o se busca una
    // sala no hay nada que decidir, y dejar la anterior debajo invitaría a
    // tocar botones que ya no aplican.
    // **Cancelar es la MISMA orden en las dos esperas**, y antes no lo era.
    // Crear no ofrecía salida ninguna, y buscar ofrecía un botón que salía de
    // una sala en la que todavía no se había entrado y navegaba a la portada:
    // la pantalla se iba y el daemon seguía, con el motor arriba y las reglas
    // puestas por una sala que la app ya no creía estar abriendo. Ahora las dos
    // le piden al daemon que pare, y él deshace lo que alcanzó a hacer.
    if (session.isWaiting) {
      // **Las dos salidas van SIN botón de cancelar, y no es un olvido.**
      // Cortar un desmontaje a la mitad deja exactamente el estado que salir
      // existe para deshacer: reglas puestas para una sala que ya no está. Ver
      // [SessionState.canCancelWait], que es de donde lo saca el propio botón.
      // Duran segundos, y el panel de pasos dice en cuál va.
      //
      // Las cuatro son `const` a propósito: esto se reconstruye con cada
      // muestra de progreso, o sea dos veces por segundo, y una espera que no
      // cambió de fase tiene que saltarse el rebuild entero. Ver [LoadingScreen].
      return switch (session.phase) {
        SessionPhase.creating => const LoadingScreen(
          flow: LoadingFlow.creating,
        ),
        SessionPhase.joining => const LoadingScreen(flow: LoadingFlow.joining),
        SessionPhase.closing => const LoadingScreen(
          flow: LoadingFlow.leaving,
          closing: true,
        ),
        _ => const LoadingScreen(flow: LoadingFlow.leaving),
      };
    }

    // **Three screens need a room, and without one they are a dead end.**
    //
    // Every control on them acts on the room, so with none there is nothing to
    // draw and nothing to press — an empty window whose only way out is the
    // task manager. It happened for real: creating a room navigated at the
    // same time as asking, so a creation that failed parked the app here.
    //
    // The call sites now wait before navigating, and [_visibleScreen] stays as
    // the floor. A dead screen is worse than any wrong screen, and the cost of
    // being sure is one comparison.
    return switch (_visibleScreen(shell, session)!) {
      AppScreen.welcome => WelcomeScreen(ambient: shell.ambient),
      // Coming from the account menu is not onboarding: the copy about being
      // halfway through signing up only belongs to the sign-up. Whether it is
      // onboarding is decided by whether there is a name yet.
      AppScreen.nickname => NicknameScreen(
        fromOnboarding: session.nickname.isEmpty,
      ),
      AppScreen.home => const HomeScreen(),
      AppScreen.gamePicker => const GamePickerScreen(),
      AppScreen.catalog => const CatalogScreen(),
      AppScreen.manualGame => const ManualGameScreen(),
      AppScreen.room => const RoomScreen(),
      AppScreen.exposure => const ExposureScreen(),
      // El `!` está defendido por [_visibleScreen], que devuelve `home` cuando
      // no hay enlace pendiente. Sin esa defensa haría falta un placeholder, y
      // un placeholder acá es una sala inventada en la pantalla que existe para
      // decir a qué sala vas a entrar.
      AppScreen.invite => InviteScreen(invite: session.invite!),
      AppScreen.settings => const SettingsScreen(),
      AppScreen.seed => const SeedScreen(),
      AppScreen.seedPassword => const SeedPasswordScreen(),
    };
  }
}

/// Los diálogos, que casi siempre no son ninguno.
///
/// # Por qué lee el estado acá y no lo recibe
///
/// Recibiéndolo era un widget NUEVO en cada emisión de cualquiera de los dos
/// cubits —treinta veces por minuto solo por el latido— para devolver el mismo
/// `SizedBox.shrink()` que ya estaba puesto.
///
/// Y no basta con subirlo acá con un `watch`: eso suscribe al estado ENTERO,
/// así que un cambio de miembros seguiría reconstruyendo la capa de diálogos
/// sin diálogo abierto. Se suscribe a lo que decide —qué diálogo hay— y solo
/// cuando hay uno se mira la sesión, que es cuando su contenido importa.
class _DialogLayer extends StatelessWidget {
  const _DialogLayer();

  @override
  Widget build(BuildContext context) {
    final AppDialog dialog = context.select<ShellCubit, AppDialog>(
      (ShellCubit c) => c.state.dialog,
    );
    if (dialog == AppDialog.none) return const SizedBox.shrink();

    final ShellState shell = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;

    return switch (dialog) {
      AppDialog.none => const SizedBox.shrink(),
      AppDialog.confirmGame =>
        session.pendingGame == null
            ? const SizedBox.shrink()
            : ConfirmGameDialog(
                game: session.pendingGame!,
                insideRoom: session.hasRoom,
              ),
      AppDialog.confirmKick =>
        shell.kickTarget == null
            ? const SizedBox.shrink()
            : ConfirmKickDialog(member: shell.kickTarget!),
      AppDialog.confirmRenew => ConfirmRenewDialog(
        membersInside: session.room?.members.length ?? 0,
      ),
      AppDialog.confirmClose => ConfirmCloseDialog(
        membersInside: session.room?.members.length ?? 0,
      ),
      AppDialog.resumeRoom =>
        session.pendingRoom == null
            ? const SizedBox.shrink()
            : ResumeRoomDialog(pending: session.pendingRoom!),
      AppDialog.trustSeed =>
        shell.trust == null
            ? const SizedBox.shrink()
            : TrustSeedDialog(request: shell.trust!),
    };
  }
}
