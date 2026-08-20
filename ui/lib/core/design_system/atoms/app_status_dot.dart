import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

/// El punto de estado.
///
/// Aparece junto al servicio activo, junto a cada miembro de la sala y en los
/// avisos. El color lo elige quien lo usa porque el significado es del
/// contexto: el verde no quiere decir lo mismo junto a un peer que junto al
/// daemon.
class AppStatusDot extends StatefulWidget {
  const AppStatusDot({
    required this.color,
    this.size = 7,
    this.pulse = false,
    this.pulseDuration = AppMotion.pulseSlow,
    this.square = false,
    super.key,
  });

  final Color color;
  final double size;

  /// El latido dice "esto está vivo AHORA", no "esto está bien". Se reserva
  /// para lo que se está midiendo en tiempo real, porque si late todo, el
  /// movimiento deja de significar nada.
  final bool pulse;

  final Duration pulseDuration;

  /// Un cuadrado en vez de un círculo, para lo que ya dejó de latir: el aviso
  /// de que el host se fue de la sala.
  final bool square;

  @override
  State<AppStatusDot> createState() => _AppStatusDotState();
}

class _AppStatusDotState extends State<AppStatusDot>
    with SingleTickerProviderStateMixin {
  /// Nulo mientras no lata, y nulable a propósito.
  ///
  /// Con `late final … = AnimationController(...)` el campo se inicializa en el
  /// primer acceso, y un punto que no late no lo toca en toda su vida: el
  /// primer acceso acaba siendo el `dispose()`. Ahí el Element ya está muerto,
  /// así que crear el Ticker busca un `TickerMode` heredado desde un árbol
  /// desactivado y revienta. La mayoría de los puntos de la app no laten —
  /// cada miembro de la sala, cada aviso — así que pasaba al salir de casi
  /// cualquier pantalla.
  AnimationController? _controller;

  /// La animación con curva, construida UNA vez y liberada en [dispose].
  ///
  /// # Por qué no vive en `build`, que es donde estaba
  ///
  /// Porque `CurvedAnimation` se suscribe al estado del controlador en su
  /// constructor, `parent.addStatusListener(_updateCurveDirection)` en
  /// `animation/animations.dart`, y lo único que deshace esa suscripción es su
  /// propio `dispose`, cuya documentación dice literalmente *"Cleans up any
  /// listeners added by this CurvedAnimation"*.
  ///
  /// Construida en cada `build`, cada reconstrucción dejaba un oyente más
  /// colgado del mismo controlador y ninguno se iba. El padre de un punto que
  /// late reconstruye cada dos segundos con el latido de la sesión: treinta
  /// oyentes por minuto y por punto, mientras la sala esté abierta. La lista no
  /// tiene techo, y `notifyStatusListeners` la recorre entera en cada vuelta
  /// del `reverse`.
  CurvedAnimation? _curva;
  Animation<double>? _opacidad;

  @override
  void initState() {
    super.initState();
    if (widget.pulse) _latir();
  }

  void _latir() {
    // La mitad de la duración, porque con `reverse: true` un ciclo son DOS
    // pasadas del controlador. Los tokens son de ciclo completo, así que sin
    // dividir late a la mitad de velocidad.
    final AnimationController c = _controller ??= AnimationController(
      vsync: this,
      duration: widget.pulseDuration ~/ 2,
    );
    // Con curva: un latido lineal se lee como un parpadeo de aviso, y esto dice
    // "vivo", no "atención".
    final CurvedAnimation curva = _curva ??= CurvedAnimation(
      parent: c,
      curve: Curves.easeInOut,
    );
    _opacidad ??= Tween<double>(begin: 1, end: 0.3).animate(curva);
    c.repeat(reverse: true);
  }

  @override
  void didUpdateWidget(AppStatusDot oldWidget) {
    super.didUpdateWidget(oldWidget);
    final AnimationController? c = _controller;
    if (widget.pulse) {
      if (c == null || !c.isAnimating) _latir();
    } else if (c != null && c.isAnimating) {
      c.stop();
      c.value = 0;
    }
  }

  @override
  void dispose() {
    // La curva PRIMERO: lo que hace es quitarse de la lista de oyentes del
    // controlador, y hacerlo después de liberarlo sería tocar algo ya muerto.
    _curva?.dispose();
    _controller?.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final Widget dot = _Dot(
      color: widget.color,
      size: widget.size,
      square: widget.square,
    );
    final Animation<double>? opacidad = _opacidad;
    if (!widget.pulse || opacidad == null) return dot;
    return FadeTransition(opacity: opacidad, child: dot);
  }
}

class _Dot extends StatelessWidget {
  const _Dot({required this.color, required this.size, required this.square});

  final Color color;
  final double size;
  final bool square;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: color,
        shape: square ? BoxShape.rectangle : BoxShape.circle,
        borderRadius: square ? BorderRadius.circular(2) : null,
      ),
    );
  }
}
