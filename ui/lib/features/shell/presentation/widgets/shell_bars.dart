import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';
import 'package:kanpachi_ui/core/design_system/atoms/kanpachi_wordmark.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:window_manager/window_manager.dart';

/// La barra de título: logotipo, cuenta y los botones de ventana.
class ShellTitleBar extends StatelessWidget {
  const ShellTitleBar({required this.nickname, super.key});

  final String nickname;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    // Es el chrome de la ventana de verdad, no una barra dibujada: arrastrarla
    // mueve la ventana y el doble clic la maximiza, como espera cualquiera que
    // use Windows. Sin esto, esconder el marco nativo dejaría una ventana que
    // no se puede mover.
    return DragToMoveArea(
      child: Container(
        height: AppSpacing.titleBarHeight,
        padding: const EdgeInsets.only(
          left: AppSpacing.x3l,
          right: AppSpacing.sm,
        ),
        decoration: BoxDecoration(
          color: colors.surfaceSunken,
          border: Border(
            bottom: BorderSide(color: colors.border, width: AppStroke.hairline),
          ),
        ),
        child: Row(
          children: <Widget>[
            const KanpachiWordmark(height: 14, opacity: 0.85),
            const Spacer(),
            _AccountButton(nickname: nickname),
            const SizedBox(width: AppSpacing.md),
            const _WindowButtons(),
          ],
        ),
      ),
    );
  }
}

/// El nombre con el que te ven, y la única forma de cambiarlo.
///
/// No es una cuenta: no hay sesión que cerrar ni perfil que ver. Por eso el
/// menú tiene una sola entrada, y decirlo en el encabezado ("ENTRAS COMO") es
/// más honesto que un avatar que insinúa que hay algo detrás.
class _AccountButton extends StatelessWidget {
  const _AccountButton({required this.nickname});

  final String nickname;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final bool open = context.select<ShellCubit, bool>(
      (ShellCubit c) => c.state.accountMenuOpen,
    );
    final String initial = nickname.isEmpty
        ? '?'
        : nickname.characters.first.toUpperCase();

    return Stack(
      clipBehavior: Clip.none,
      alignment: Alignment.topRight,
      children: <Widget>[
        MouseRegion(
          cursor: SystemMouseCursors.click,
          child: GestureDetector(
            onTap: shell.toggleAccountMenu,
            child: Container(
              padding: const EdgeInsets.fromLTRB(4, 4, 9, 4),
              decoration: BoxDecoration(
                color: open ? colors.surface : Colors.transparent,
                borderRadius: AppRadius.pill,
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  Container(
                    width: 22,
                    height: 22,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      color: colors.accent,
                      shape: BoxShape.circle,
                    ),
                    child: Text(
                      initial,
                      style: context.type.monoXxs.copyWith(
                        color: colors.accentInk,
                        fontSize: 10,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  const SizedBox(width: 7),
                  Text(
                    nickname.isEmpty ? 'sin nombre' : nickname,
                    style: context.type.labelSm.copyWith(color: colors.text),
                  ),
                ],
              ),
            ),
          ),
        ),
        if (open)
          Positioned(
            top: 32,
            right: 0,
            child: _AccountMenu(nickname: nickname),
          ),
      ],
    );
  }
}

class _AccountMenu extends StatelessWidget {
  const _AccountMenu({required this.nickname});

  final String nickname;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return TweenAnimationBuilder<double>(
      duration: AppMotion.hover,
      curve: AppMotion.enter,
      tween: Tween<double>(begin: 0, end: 1),
      builder: (BuildContext context, double t, Widget? child) =>
          Opacity(opacity: t, child: child),
      child: Container(
        width: 210,
        padding: const EdgeInsets.all(AppSpacing.xl),
        decoration: BoxDecoration(
          color: colors.surface,
          borderRadius: AppRadius.allMd,
          border: Border.all(color: colors.border, width: AppStroke.hairline),
          boxShadow: <BoxShadow>[
            BoxShadow(
              color: colors.shadow,
              blurRadius: 40,
              offset: const Offset(0, 16),
            ),
          ],
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const AppKicker('Entras como', small: true),
            const SizedBox(height: 7),
            Text(
              nickname.isEmpty ? 'sin nombre' : nickname,
              style: context.type.strong.copyWith(color: colors.text),
            ),
            const SizedBox(height: AppSpacing.xl),
            SizedBox(
              width: double.infinity,
              child: AppButton(
                label: 'Cambiar nombre',
                variant: AppButtonVariant.quiet,
                height: 36,
                onPressed: () =>
                    context.read<ShellCubit>().go(AppScreen.nickname),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Minimizar, maximizar y cerrar.
///
/// Cerrar NO cierra la sala: el daemon la sostiene y queda el icono en la
/// bandeja. Es lo que hace que se pueda seguir jugando con la ventana cerrada,
/// y por eso el botón lleva a la pantalla de bandeja en vez de matar el
/// proceso.
class _WindowButtons extends StatelessWidget {
  const _WindowButtons();

  @override
  Widget build(BuildContext context) {
    return Row(
      children: <Widget>[
        AppIconButton(
          icon: Icons.remove,
          tooltip: 'Minimizar',
          onPressed: windowManager.minimize,
        ),
        AppIconButton(
          icon: Icons.crop_square,
          tooltip: 'Maximizar',
          iconSize: 11,
          onPressed: () async {
            if (await windowManager.isMaximized()) {
              await windowManager.unmaximize();
            } else {
              await windowManager.maximize();
            }
          },
        ),
        AppIconButton(
          icon: Icons.close,
          tooltip: 'Cerrar a la bandeja',
          // Cerrar NO cierra la sala: el daemon la sostiene y queda el icono
          // en la bandeja. Por eso lleva a esa pantalla en vez de matar el
          // proceso; matarlo sería tirar la partida de todos.
          onPressed: () => context.read<ShellCubit>().go(AppScreen.tray),
        ),
      ],
    );
  }
}

/// La barra de estado del pie: el daemon y dónde estás conectado.
class ShellStatusBar extends StatelessWidget {
  const ShellStatusBar({required this.right, super.key});

  /// El dato de la derecha: el adaptador y tu IP dentro de la sala, o el seed
  /// cuando no hay sala.
  final String right;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      height: AppSpacing.statusBarHeight,
      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.x3l),
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        border: Border(
          top: BorderSide(color: colors.border, width: AppStroke.hairline),
        ),
      ),
      child: Row(
        children: <Widget>[
          AppStatusDot(color: colors.ok, pulse: true),
          const SizedBox(width: AppSpacing.md),
          Text(
            'Servicio activo',
            style: context.type.labelSm.copyWith(
              color: colors.textMuted,
              fontWeight: FontWeight.w400,
            ),
          ),
          const Spacer(),
          Text(
            right,
            style: context.type.monoSm.copyWith(color: colors.textMuted),
          ),
        ],
      ),
    );
  }
}
