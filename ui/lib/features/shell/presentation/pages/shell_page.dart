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
        const _CurrentScreen(),
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

/// The notice for whatever the user asked for last and did not happen.
class _FailureLayer extends StatelessWidget {
  const _FailureLayer();

  @override
  Widget build(BuildContext context) {
    final ActionFailure? failure = context.watch<SessionCubit>().state.failure;
    if (failure == null) return const SizedBox.shrink();

    return Positioned(
      left: AppSpacing.x4l,
      right: AppSpacing.x4l,
      bottom: AppSpacing.x4l,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 720),
        child: FailureNotice(
          failure: failure,
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
    if (session.phase == SessionPhase.creating) {
      return const ProgressScreen.creating();
    }
    if (session.phase == SessionPhase.joining) {
      return ProgressScreen(
        title: 'Buscando la sala…',
        note:
            'Presentando tu equipo con los demás miembros. El tráfico del '
            'juego nunca pasa por el servidor.',
        onCancel: () {
          context.read<SessionCubit>().leave();
          context.read<ShellCubit>().go(AppScreen.home);
        },
      );
    }

    // **Three screens need a room, and without one they are a dead end.**
    //
    // Every control on them acts on the room, so with none there is nothing to
    // draw and nothing to press — an empty window whose only way out is the
    // task manager. It happened for real: creating a room navigated at the
    // same time as asking, so a creation that failed parked the app here.
    //
    // The call sites now wait before navigating, and this stays as the floor.
    // A dead screen is worse than any wrong screen, and the cost of being sure
    // is one comparison.
    if (session.room == null &&
        (shell.screen == AppScreen.room ||
            shell.screen == AppScreen.exposure ||
            shell.screen == AppScreen.invite)) {
      return const HomeScreen();
    }

    return switch (shell.screen) {
      AppScreen.welcome => WelcomeScreen(ambient: shell.ambient),
      AppScreen.nickname => const NicknameScreen(),
      AppScreen.home => const HomeScreen(),
      AppScreen.gamePicker => const GamePickerScreen(),
      AppScreen.catalog => const CatalogScreen(),
      AppScreen.manualGame => const ManualGameScreen(),
      AppScreen.room => const RoomScreen(),
      AppScreen.exposure => const ExposureScreen(),
      AppScreen.invite => InviteScreen(
        code: session.room?.code ?? 'A7K2-M9QX',
        roomName: session.room?.name ?? 'La Guarida',
      ),
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
    };
  }
}
