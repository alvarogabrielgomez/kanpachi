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
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/update/presentation/widgets/update_notice.dart';
import 'package:window_manager/window_manager.dart';

/// La barra de título: logotipo, cuenta y los botones de ventana.
///
/// # Por qué el área de arrastre NO envuelve la barra entera
///
/// Envolvía, y costaba dos cosas a la vez. `DragToMoveArea` es un detector de
/// arrastre, así que competía en la arena de gestos con el botón de cuenta que
/// tenía debajo: un clic con el ratón moviéndose un píxel se lo llevaba el
/// arrastre, la ventana daba un salto y el menú no se abría. Desde fuera se ve
/// como "hay que acertarle", que es justo lo que se reportó.
///
/// Ahora el arrastre ocupa solo la franja vacía de la izquierda, y los
/// controles son hermanos suyos. Lo que se pierde: arrastrar la ventana desde
/// encima del nombre. Lo que se gana: que el nombre se pueda pulsar siempre. Un
/// control dentro de una zona de arrastre es un control que a veces no
/// responde, y eso no se arregla con más área.
class ShellTitleBar extends StatelessWidget {
  const ShellTitleBar({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      height: AppSpacing.titleBarHeight,
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        border: Border(
          bottom: BorderSide(color: colors.border, width: AppStroke.hairline),
        ),
      ),
      child: Row(
        children: <Widget>[
          // La franja de arrastre, que se queda con todo el hueco sobrante: es
          // el chrome de la ventana de verdad, así que arrastrarla la mueve y
          // el doble clic la maximiza, como espera cualquiera que use Windows.
          const Expanded(
            child: DragToMoveArea(
              child: Padding(
                padding: EdgeInsets.only(left: AppSpacing.x3l),
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: KanpachiWordmark(height: 14, opacity: 0.85),
                ),
              ),
            ),
          ),
          const _AccountButton(),
          const SizedBox(width: AppSpacing.md),
          const _WindowButtons(),
          const SizedBox(width: AppSpacing.sm),
        ],
      ),
    );
  }
}

/// El nombre con el que te ven, cómo cambiarlo, y el engranaje.
///
/// No es una cuenta: no hay sesión que cerrar ni perfil que ver. Decirlo en el
/// encabezado ("ENTRAS COMO") es más honesto que un avatar que insinúa que hay
/// algo detrás.
///
/// La configuración cuelga de acá y no de la portada a propósito: nada de lo
/// que hay dentro hace falta para jugar, y un botón de ajustes en la primera
/// pantalla dice lo contrario. Ver [SettingsScreen].
class _AccountButton extends StatefulWidget {
  const _AccountButton();

  @override
  State<_AccountButton> createState() => _AccountButtonState();
}

class _AccountButtonState extends State<_AccountButton> {
  /// Se marca al pasar por encima, igual que los botones de ventana de al lado.
  ///
  /// No es adorno: era lo único de la barra que se podía pulsar sin decirlo, y
  /// un objetivo que no se anuncia se busca a tientas.
  bool _hovered = false;

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
    // El nombre se toma de aquí y no baja como parámetro, para que un latido
    // que no lo cambia no reconstruya la barra de título entera. Ver la
    // cabecera de [ShellPage].
    final String nickname = context.select<SessionCubit, String>(
      (SessionCubit c) => c.state.nickname,
    );
    final String initial = nickname.isEmpty
        ? '?'
        : nickname.characters.first.toUpperCase();

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
              child: _AccountMenu(nickname: nickname),
            ),
          ],
        ),
        child: CompositedTransformTarget(
          link: _link,
          child: MouseRegion(
            cursor: SystemMouseCursors.click,
            onEnter: (_) => setState(() => _hovered = true),
            onExit: (_) => setState(() => _hovered = false),
            child: GestureDetector(
              // **`opaque`, y esa palabra es media corrección.** Por omisión un
              // `GestureDetector` difiere al hijo, así que solo respondían los
              // píxeles PINTADOS: el círculo del avatar y las letras del
              // nombre. Los huecos entre ellos, el relleno y la franja de
              // arriba y abajo no eran el botón, aunque lo parecieran. Por eso
              // había que apuntar.
              behavior: HitTestBehavior.opaque,
              onTap: shell.toggleAccountMenu,
              child: AnimatedContainer(
                duration: AppMotion.hover,
                // Alto de la barra ENTERA. El objetivo llegaba a 30 px en una
                // barra de 44, así que los 7 de arriba y los 7 de abajo caían
                // fuera aunque el ratón estuviera encima del nombre.
                height: AppSpacing.titleBarHeight,
                padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md),
                decoration: BoxDecoration(
                  color: _hovered ? colors.surface : Colors.transparent,
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
                      nickname.isEmpty ? 'sin nombre' : nickname,
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
            Row(
              children: <Widget>[
                const Expanded(child: AppKicker('Entras como', small: true)),
                // El engranaje va acá arriba y no como segundo botón: lo de
                // abajo es lo que se viene a hacer, y una lista de dos botones
                // del mismo tamaño los pone a competir. Un icono al lado del
                // encabezado se encuentra cuando se busca y no estorba cuando
                // no.
                AppIconButton(
                  tooltip: 'Configuración',
                  icon: Icons.settings_outlined,
                  width: 26,
                  height: 22,
                  iconSize: 15,
                  onPressed: () =>
                      context.read<ShellCubit>().go(AppScreen.settings),
                ),
              ],
            ),
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
///
/// **La mitad izquierda dice lo que se midió, no lo que se espera.** Decía
/// "Servicio activo" fijo en el código, así que con el daemon caído la ventana
/// enseñaba a la vez el cartel de que no hay conexión y un punto verde
/// diciendo lo contrario. De las dos, la que mentía era esta, y una barra de
/// estado que miente es peor que no tenerla: es el sitio al que se mira para
/// saber si hace falta mirar a otro lado.
class ShellStatusBar extends StatelessWidget {
  const ShellStatusBar({
    required this.right,
    required this.rightIsSeed,
    required this.daemonDown,
    super.key,
  });

  /// El dato de la derecha: el adaptador y tu IP dentro de la sala, o el seed
  /// cuando no hay sala.
  final String right;

  /// Si lo de la derecha es el SERVIDOR, y no el adaptador de una sala.
  ///
  /// Decide dos cosas: qué dice el globo, y si el sitio se puede pulsar. Con
  /// sala abierta ahí no hay ningún servidor escrito —hay un adaptador y una
  /// dirección—, así que llevar de ahí a la pantalla de cambiar el servidor
  /// sería mandar a alguien a otra cosa que la que está mirando.
  final bool rightIsSeed;

  /// No se pudo hablar con el servicio.
  final bool daemonDown;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      height: AppSpacing.statusBarHeight,
      // El relleno de la derecha lo pone ahora [_RightSlot], que necesita
      // llegar hasta el borde para que su realce no deje una franja muerta.
      padding: const EdgeInsets.only(left: AppSpacing.x3l),
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        border: Border(
          top: BorderSide(color: colors.border, width: AppStroke.hairline),
        ),
      ),
      child: Row(
        children: <Widget>[
          // Sin latido cuando no hay servicio: el pulso es lo que dice "esto
          // está vivo", y es justo lo que no se puede afirmar.
          AppStatusDot(
            color: daemonDown ? colors.warn : colors.ok,
            pulse: !daemonDown,
          ),
          const SizedBox(width: AppSpacing.md),
          Text(
            daemonDown ? 'Sin servicio' : 'Servicio activo',
            style: context.type.statusLabel.copyWith(color: colors.textMuted),
          ),
          const Spacer(),
          // A la izquierda del dato del adaptador y no a la derecha del todo:
          // ese dato es el ancla fija de la barra —siempre está, siempre en el
          // mismo sitio— y meterle algo a la derecha lo movería el día que
          // aparece una versión nueva. El aviso empuja hacia dentro, y cuando
          // no hay ninguno la barra queda exactamente como estaba.
          const UpdateNotice(),
          _RightSlot(text: right, isSeed: rightIsSeed),
        ],
      ),
    );
  }
}

/// La esquina de la derecha: qué dice, qué explica, y a dónde lleva.
///
/// # Por qué se puede pulsar
///
/// Porque es el único sitio de la app donde el servidor está escrito siempre, y
/// no llevaba a ninguna parte: para cambiarlo había que abrir el menú de
/// cuenta, entrar en Configuración y encontrar la tarjeta. Un dato que se lee a
/// diario y que se cambia tres pantallas más allá es un dato que invita a
/// buscarlo por su cuenta.
///
/// # Por qué el globo, si el nombre ya está escrito
///
/// Porque el nombre no dice qué es esa máquina. `kanpachi.accentio.dev` ahí
/// abajo se lee como «a dónde estás conectado», y lo que es de verdad es el
/// punto de encuentro donde se abren TUS salas: quien lo mire con una sala de
/// otro abierta se llevaría la idea contraria. Espera medio segundo antes de
/// aparecer, que es lo que separa a quien lo está mirando de quien pasa el
/// ratón camino de otro sitio.
class _RightSlot extends StatefulWidget {
  const _RightSlot({required this.text, required this.isSeed});

  final String text;
  final bool isSeed;

  @override
  State<_RightSlot> createState() => _RightSlotState();
}

class _RightSlotState extends State<_RightSlot> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final Widget texto = AnimatedContainer(
      duration: AppMotion.hover,
      height: AppSpacing.statusBarHeight,
      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.x3l),
      alignment: Alignment.center,
      color: _hovered && widget.isSeed ? colors.surface : Colors.transparent,
      child: Text(
        widget.text,
        style: context.type.statusMono.copyWith(
          color: _hovered && widget.isSeed ? colors.text : colors.textMuted,
        ),
      ),
    );

    return Tooltip(
      message: widget.isSeed
          ? 'El servidor de encuentro donde abres tus salas. Pulsa para '
                'cambiarlo.'
          : 'Tu adaptador virtual y tu dirección dentro de esta sala.',
      waitDuration: const Duration(milliseconds: 500),
      child: MouseRegion(
        cursor: widget.isSeed
            ? SystemMouseCursors.click
            : SystemMouseCursors.basic,
        onEnter: (_) => setState(() => _hovered = true),
        onExit: (_) => setState(() => _hovered = false),
        child: GestureDetector(
          // `opaque`, como el botón de cuenta: sin esto el objetivo son las
          // letras y no la zona, y una franja de 38 px de alto se convierte en
          // una de 12 que hay que apuntar.
          behavior: HitTestBehavior.opaque,
          onTap: widget.isSeed
              ? () => context.read<ShellCubit>().go(AppScreen.seed)
              : null,
          child: texto,
        ),
      ),
    );
  }
}
