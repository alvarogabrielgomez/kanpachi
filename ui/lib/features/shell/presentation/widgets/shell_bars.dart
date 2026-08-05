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
class _AccountButton extends StatefulWidget {
  const _AccountButton({required this.nickname});

  final String nickname;

  @override
  State<_AccountButton> createState() => _AccountButtonState();
}

class _AccountButtonState extends State<_AccountButton> {
  /// El panel se dibuja en el Overlay de la raíz, no aquí.
  ///
  /// Colgado de la barra de título era inalcanzable. Un `RenderFlex` pinta a
  /// sus hijos en orden y la barra es el PRIMER hijo del `Column` del marco,
  /// así que el cuerpo se pintaba encima del desbordamiento; y el hit test
  /// recorre esos hijos al revés, así que el `Scrollable` de la pantalla —que
  /// es `HitTestBehavior.opaque`— se tragaba el toque antes de que llegara.
  /// "Cambiar nombre" sólo respondía en la franja de 5 px que aún caía dentro
  /// de la barra. Y al vivir dentro del `DragToMoveArea`, arrastrar sobre el
  /// panel movía la ventana.
  final LayerLink _link = LayerLink();
  final OverlayPortalController _portal = OverlayPortalController();

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final String initial = widget.nickname.isEmpty
        ? '?'
        : widget.nickname.characters.first.toUpperCase();

    return BlocListener<ShellCubit, ShellState>(
      listenWhen: (ShellState a, ShellState b) =>
          a.accountMenuOpen != b.accountMenuOpen,
      // Se sincroniza en el listener y NUNCA dentro del build: mostrar u
      // ocultar un overlay marca el árbol como sucio, y hacerlo mientras se
      // construye revienta.
      listener: (BuildContext context, ShellState state) {
        state.accountMenuOpen ? _portal.show() : _portal.hide();
      },
      child: OverlayPortal(
        controller: _portal,
        overlayChildBuilder: (BuildContext context) => Stack(
          children: <Widget>[
            // Clicar fuera cierra. Va debajo del panel y por encima de todo lo
            // demás, que es lo que se espera de un menú.
            Positioned.fill(
              child: GestureDetector(
                behavior: HitTestBehavior.opaque,
                onTap: shell.closeAccountMenu,
              ),
            ),
            // Anclado por `LayerLink` y no por coordenadas: el panel se pega al
            // borde inferior derecho de la píldora, sin depender de cuántos
            // botones de ventana haya a su derecha.
            CompositedTransformFollower(
              link: _link,
              targetAnchor: Alignment.bottomRight,
              followerAnchor: Alignment.topRight,
              offset: const Offset(0, 2),
              child: _AccountMenu(nickname: widget.nickname),
            ),
          ],
        ),
        child: CompositedTransformTarget(
          link: _link,
          child: MouseRegion(
            cursor: SystemMouseCursors.click,
            child: GestureDetector(
              onTap: shell.toggleAccountMenu,
              child: Container(
                padding: const EdgeInsets.fromLTRB(4, 4, 9, 4),
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
                        style: context.type.labelSm.copyWith(
                          color: colors.accentInk,
                          fontSize: 10,
                          fontWeight: FontWeight.w700,
                          height: 1,
                        ),
                      ),
                    ),
                    const SizedBox(width: 7),
                    Text(
                      widget.nickname.isEmpty ? 'sin nombre' : widget.nickname,
                      style: context.type.labelSm.copyWith(color: colors.text),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
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
      // Baja al aparecer, no sólo se enciende: el desplazamiento es lo que lo
      // ata visualmente al botón del que sale.
      builder: (BuildContext context, double t, Widget? child) => Opacity(
        opacity: t,
        child: Transform.translate(
          offset: Offset(0, -6 * (1 - t)),
          child: child,
        ),
      ),
      child: Container(
        width: 210,
        padding: const EdgeInsets.all(AppSpacing.xl),
        decoration: BoxDecoration(
          color: colors.surface,
          borderRadius: AppRadius.allMd,
          border: Border.all(color: colors.border, width: AppStroke.hairline),
          boxShadow: <BoxShadow>[
            BoxShadow(
              // Sombra propia y no el token global: el del tema es la sombra
              // de la ventana flotando en la maqueta, mucho más densa que la
              // de un menú. 34 de blur es el equivalente exacto del 40px de
              // CSS, que mide sigma y no radio.
              color: colors.shadowMenu,
              blurRadius: 34,
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
                variant: AppButtonVariant.quietSunken,
                height: 32,
                textStyle: context.type.labelSm,
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
    final colors = context.colors;
    return Row(
      children: <Widget>[
        AppIconButton(
          tooltip: 'Minimizar',
          glyph: Container(
            width: 11,
            height: AppStroke.hairline,
            color: colors.textMuted,
          ),
          onPressed: windowManager.minimize,
        ),
        AppIconButton(
          tooltip: 'Maximizar',
          glyph: Container(
            width: 10,
            height: 10,
            decoration: BoxDecoration(
              border: Border.all(
                color: colors.textMuted,
                width: AppStroke.hairline,
              ),
              borderRadius: BorderRadius.circular(2),
            ),
          ),
          onPressed: () async {
            if (await windowManager.isMaximized()) {
              await windowManager.unmaximize();
            } else {
              await windowManager.maximize();
            }
          },
        ),
        AppIconButton(
          tooltip: 'Cerrar a la bandeja',
          glyph: _CloseGlyph(color: colors.textMuted),
          // Cerrar NO cierra la sala. `close()` no mata el proceso: la ventana
          // tiene puesto `preventClose`, así que dispara `onWindowClose` y el
          // TrayBridge la esconde dejando el icono en la bandeja. Matarla sería
          // tirar la partida de todos.
          onPressed: windowManager.close,
        ),
      ],
    );
  }
}

/// La cruz: dos rayas de un píxel cruzadas.
class _CloseGlyph extends StatelessWidget {
  const _CloseGlyph({required this.color});

  final Color color;

  static const double _quarterTurn = 3.141592653589793 / 4;

  @override
  Widget build(BuildContext context) {
    final Widget raya = Container(
      width: 12,
      height: AppStroke.hairline,
      color: color,
    );
    return SizedBox(
      width: 12,
      height: 12,
      child: Stack(
        alignment: Alignment.center,
        children: <Widget>[
          Transform.rotate(angle: _quarterTurn, child: raya),
          Transform.rotate(angle: -_quarterTurn, child: raya),
        ],
      ),
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
            style: context.type.statusLabel.copyWith(color: colors.textMuted),
          ),
          const Spacer(),
          Text(
            right,
            style: context.type.statusMono.copyWith(color: colors.textMuted),
          ),
        ],
      ),
    );
  }
}
