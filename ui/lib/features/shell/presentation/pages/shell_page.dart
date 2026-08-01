import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_ambient_background.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/onboarding/presentation/pages/onboarding_pages.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
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
          Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpacing.x7l,
                vertical: AppSpacing.x8l,
              ),
              child: const _Window(),
            ),
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
  /// cuando no, a qué seed estás apuntando. Nunca las dos, porque la barra es
  /// una línea y llenarla de datos la vuelve ruido.
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

/// El área que cambia. Su altura está acotada como en el diseño para que la
/// ventana no dé saltos entre una pantalla corta y una larga.
class _WindowBody extends StatelessWidget {
  const _WindowBody();

  @override
  Widget build(BuildContext context) {
    final ShellState shell = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;

    return ConstrainedBox(
      constraints: const BoxConstraints(minHeight: 240, maxHeight: 596),
      child: Stack(
        children: <Widget>[
          Positioned.fill(
            child: SingleChildScrollView(
              child: _screenFor(context, shell, session),
            ),
          ),
        ],
      ),
    );
  }

  Widget _screenFor(
    BuildContext context,
    ShellState shell,
    SessionState session,
  ) {
    // Las esperas ganan a la pantalla elegida: mientras se crea o se busca una
    // sala no hay nada que decidir, y dejar la pantalla anterior debajo
    // invitaría a tocar botones que ya no aplican.
    if (session.phase == SessionPhase.creating) {
      return const ProgressScreen.creating();
    }
    if (session.phase == SessionPhase.joining) {
      return ProgressScreen(
        title: 'Buscando la sala…',
        note: 'Presentando tu equipo con los demás miembros. El tráfico del '
            'juego nunca pasa por el servidor.',
        onCancel: () => context.read<SessionCubit>().leave(),
      );
    }

    return switch (shell.screen) {
      AppScreen.welcome => WelcomeScreen(ambient: shell.ambient),
      AppScreen.nickname => const NicknameScreen(),
      AppScreen.home ||
      AppScreen.gamePicker ||
      AppScreen.catalog ||
      AppScreen.manualGame ||
      AppScreen.room ||
      AppScreen.invite ||
      AppScreen.tray =>
        _Pending(screen: shell.screen),
    };
  }
}

/// Marcador de una pantalla que todavía no está escrita.
///
/// Existe para que el árbol compile y la app se pueda abrir mientras las
/// pantallas se van escribiendo. Dice qué falta en vez de fingir que está
/// hecho: una pantalla vacía sin explicación se confunde con un bug.
class _Pending extends StatelessWidget {
  const _Pending({required this.screen});

  final AppScreen screen;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Padding(
      padding: const EdgeInsets.all(AppSpacing.x10l),
      child: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(
              'Pantalla en construcción',
              style: context.type.sectionTitle.copyWith(color: colors.text),
            ),
            const SizedBox(height: AppSpacing.md),
            Text(
              screen.name,
              style: context.type.monoSm.copyWith(color: colors.textMuted),
            ),
          ],
        ),
      ),
    );
  }
}
