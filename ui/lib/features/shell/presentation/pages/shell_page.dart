import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/action_failure.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/failure_notice.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/features/games/presentation/pages/game_picker_page.dart';
import 'package:kanpachi_ui/features/games/presentation/pages/manual_game_page.dart';
import 'package:kanpachi_ui/features/home/presentation/pages/home_page.dart';
import 'package:kanpachi_ui/features/invite/presentation/pages/invite_pages.dart';
import 'package:kanpachi_ui/features/onboarding/presentation/pages/onboarding_pages.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/exposure_page.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/room_page.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/room_dialogs.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
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

  @override
  Widget build(BuildContext context) {
    final SessionState session = context.watch<SessionCubit>().state;

    return Scaffold(
      backgroundColor: context.colors.surface,
      body: Column(
        children: <Widget>[
          ShellTitleBar(nickname: session.nickname),
          const Expanded(child: _WindowBody()),
          ShellStatusBar(
            right: _statusRight(session),
            daemonDown: session.daemonDown,
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
    if (room == null) return 'kanpachi.accentio.dev';
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
    final ShellState shell = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;

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
        _DialogLayer(shell: shell, session: session),
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
            if (state.room != null) {
              if (_sinSala.contains(ahora)) shell.go(AppScreen.room);
              return;
            }
            if (ahora == AppScreen.room || ahora == AppScreen.exposure) {
              shell.go(AppScreen.home);
            }
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
            // Se fue el enlace sin haber entrado a nada: se vuelve de donde se
            // vino. Con sala abierta el otro oyente ya manda.
            if (shell.state.screen == AppScreen.invite && !state.hasRoom) {
              shell.go(AppScreen.home);
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
  if (shell.screen == AppScreen.invite) {
    return session.invite != null ? AppScreen.invite : AppScreen.home;
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
      void cancelar() {
        context.read<SessionCubit>().cancelPending();
        context.read<ShellCubit>().go(AppScreen.home);
      }

      if (session.phase == SessionPhase.creating) {
        return ProgressScreen.creating(onCancel: cancelar);
      }
      return ProgressScreen(
        title: 'Buscando la sala…',
        note:
            'Presentando tu equipo con los demás miembros. El tráfico del '
            'juego nunca pasa por el servidor.',
        onCancel: cancelar,
      );
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
    };
  }
}

class _DialogLayer extends StatelessWidget {
  const _DialogLayer({required this.shell, required this.session});

  final ShellState shell;
  final SessionState session;

  @override
  Widget build(BuildContext context) {
    return switch (shell.dialog) {
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
    };
  }
}
