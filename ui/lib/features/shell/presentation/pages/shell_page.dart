import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_ambient_background.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/games/presentation/pages/game_picker_page.dart';
import 'package:kanpachi_ui/features/games/presentation/pages/manual_game_page.dart';
import 'package:kanpachi_ui/features/home/presentation/pages/home_page.dart';
import 'package:kanpachi_ui/features/invite/presentation/pages/invite_pages.dart';
import 'package:kanpachi_ui/features/onboarding/presentation/pages/onboarding_pages.dart';
import 'package:kanpachi_ui/features/room/presentation/pages/room_page.dart';
import 'package:kanpachi_ui/features/room/presentation/widgets/room_dialogs.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/prototype_dock.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/shell_bars.dart';

/// El marco de la aplicación: la tarjeta flotando sobre el lienzo, con su
/// barra de título arriba y la de estado abajo.
///
/// El marco no cambia nunca. Lo único que cambia es lo de dentro, y eso es lo
/// que hace que la app se sienta un solo sitio y no una sucesión de pantallas
/// distintas: el código de la sala y el estado del servicio siguen visibles
/// mientras se elige un juego o se lee la biblioteca.
class ShellPage extends StatelessWidget {
  const ShellPage({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellState shell = context.watch<ShellCubit>().state;

    return Scaffold(
      backgroundColor: colors.background,
      body: Stack(
        children: <Widget>[
          AppAmbientBackground(enabled: shell.ambient),
          Column(
            children: <Widget>[
              Expanded(
                child: SingleChildScrollView(
                  child: Padding(
                    padding: const EdgeInsets.symmetric(
                      horizontal: AppSpacing.x7l,
                      vertical: AppSpacing.x8l,
                    ),
                    child: const Center(child: _Window()),
                  ),
                ),
              ),
              // Sólo en debug. Es la barra del prototipo, no una función de la
              // app: en una build de release no existe ni ocupa sitio.
              if (kDebugMode) const PrototypeDock(),
            ],
          ),
        ],
      ),
    );
  }
}

class _Window extends StatelessWidget {
  const _Window();

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final SessionState session = context.watch<SessionCubit>().state;

    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: AppSpacing.shellWidth),
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: colors.surface,
          borderRadius: AppRadius.allLg,
          border: Border.all(color: colors.border, width: AppStroke.hairline),
          boxShadow: <BoxShadow>[
            BoxShadow(
              color: colors.shadow,
              blurRadius: 84,
              spreadRadius: -38,
              offset: const Offset(0, 46),
            ),
          ],
        ),
        child: ClipRRect(
          borderRadius: AppRadius.allLg,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              ShellTitleBar(nickname: session.nickname),
              const _WindowBody(),
              ShellStatusBar(right: _statusRight(session)),
            ],
          ),
        ),
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

    return ConstrainedBox(
      // Acotada como en el diseño para que la ventana no dé saltos entre una
      // pantalla corta y una larga.
      constraints: const BoxConstraints(minHeight: 240, maxHeight: 596),
      child: Stack(
        children: <Widget>[
          const Positioned.fill(
            child: SingleChildScrollView(child: _CurrentScreen()),
          ),
          // Dentro del marco y no como ruta aparte: los tres diálogos
          // confirman algo que cambia la sala que se ve por detrás, y dejarla
          // visible tras el velo es lo que da contexto a qué se confirma.
          _DialogLayer(shell: shell, session: session),
        ],
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
        note: 'Presentando tu equipo con los demás miembros. El tráfico del '
            'juego nunca pasa por el servidor.',
        onCancel: () {
          context.read<SessionCubit>().leave();
          context.read<ShellCubit>().go(AppScreen.home);
        },
      );
    }

    return switch (shell.screen) {
      AppScreen.welcome => WelcomeScreen(ambient: shell.ambient),
      AppScreen.nickname => const NicknameScreen(),
      AppScreen.home => const HomeScreen(),
      AppScreen.gamePicker => const GamePickerScreen(),
      AppScreen.catalog => const CatalogScreen(),
      AppScreen.manualGame => const ManualGameScreen(),
      AppScreen.room => const RoomScreen(),
      AppScreen.invite => InviteScreen(
          code: session.room?.code ?? 'A7K2-M9QX',
          roomName: session.room?.name ?? 'La Guarida',
        ),
      AppScreen.tray => const TrayScreen(),
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
      AppDialog.confirmGame => session.pendingGame == null
          ? const SizedBox.shrink()
          : ConfirmGameDialog(
              game: session.pendingGame!,
              insideRoom: session.hasRoom,
            ),
      AppDialog.confirmKick => shell.kickTarget == null
          ? const SizedBox.shrink()
          : ConfirmKickDialog(member: shell.kickTarget!),
      AppDialog.confirmRenew => ConfirmRenewDialog(
          membersInside: session.room?.members.length ?? 0,
        ),
    };
  }
}
