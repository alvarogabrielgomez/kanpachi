import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// El tono de un aviso.
enum AppNoticeTone {
  /// Ámbar. Algo que el usuario puede arreglar y que le conviene arreglar:
  /// el firewall apagado, un puerto abierto en el router, una regla que dejó
  /// el propio juego. Nunca rojo — no hay nada roto, hay algo mejorable, y el
  /// rojo pediría una urgencia que no existe.
  warn,

  /// Neutro. Un hecho que cambia lo que se puede hacer pero que no es culpa
  /// de nadie: el host se fue de la sala.
  neutral,

  /// Confirmación de algo que ya se hizo: la regla quedó desactivada.
  done,

  /// Rojo. Algo que el usuario PIDIÓ y no ocurrió: la sala no se creó, el
  /// código no se renovó, no se pudo salir.
  ///
  /// Es el único tono que rompe la regla de "nunca rojo", y la rompe con
  /// motivo: los otros tres hablan de cosas que Kanpachi observó por su
  /// cuenta, y este habla de una orden que falló. Ahí la urgencia sí existe,
  /// porque hay algo que reintentar y alguien esperando.
  error,
}

/// El aviso: punto de color, título y explicación.
///
/// La explicación es obligatoria a propósito. Un aviso que sólo dice "tu
/// firewall está apagado" deja al usuario con el susto y sin salida; el
/// diseño siempre cuenta qué implica y qué hacer, y esta clase hace que no se
/// pueda escribir uno sin eso.
class AppNotice extends StatelessWidget {
  const AppNotice({
    required this.title,
    required this.body,
    this.tone = AppNoticeTone.warn,
    this.actions,
    this.pulse = false,
    this.titleStyle,
    super.key,
  });

  /// La variante de una sola línea, sin título: el estado de la red.
  const AppNotice.line({
    required this.body,
    this.tone = AppNoticeTone.warn,
    this.pulse = false,
    super.key,
  }) : title = null,
       actions = null,
       titleStyle = null;

  final String? title;

  /// Qué implica y, cuando corresponde, qué hacer.
  final Widget body;

  final AppNoticeTone tone;
  final List<Widget>? actions;

  /// Late cuando el aviso describe algo que está pasando ahora mismo, como una
  /// reconexión en curso.
  final bool pulse;

  /// Los avisos de salud de la portada llevan el título un punto por debajo
  /// del de la sala: son dos y van seguidos, y con el tamaño del otro pesan
  /// más que la acción que tienen al lado.
  final TextStyle? titleStyle;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final (Color background, Color dot) = switch (tone) {
      AppNoticeTone.warn => (colors.warnSurface, colors.warn),
      AppNoticeTone.neutral => (colors.chip, colors.idle),
      AppNoticeTone.done => (colors.chip, colors.ok),
      AppNoticeTone.error => (colors.dangerSurface, colors.danger),
    };

    final bool oneLine = title == null && actions == null;

    // El diseño da CUATRO cajas distintas, no una. Los dos ternarios que había
    // devolvían lo mismo en las dos ramas, así que todos los avisos salían
    // iguales. La forma la marcan la presencia de acciones y el tono, que es
    // lo que distingue "una línea de estado" de "algo que hay que decidir".
    final (BorderRadius radius, EdgeInsets padding) = switch (this) {
      _ when oneLine => (
        AppRadius.allLg,
        const EdgeInsets.symmetric(horizontal: AppSpacing.x3l, vertical: 13),
      ),
      _ when actions != null => (
        AppRadius.allXl,
        const EdgeInsets.symmetric(horizontal: 17, vertical: 15),
      ),
      _ when tone == AppNoticeTone.neutral => (
        AppRadius.allXl,
        const EdgeInsets.symmetric(
          horizontal: AppSpacing.x4l,
          vertical: AppSpacing.x3l,
        ),
      ),
      _ => (
        AppRadius.allLg,
        const EdgeInsets.symmetric(
          horizontal: AppSpacing.x3l,
          vertical: AppSpacing.xxl,
        ),
      ),
    };

    return Container(
      width: double.infinity,
      padding: padding,
      decoration: BoxDecoration(color: background, borderRadius: radius),
      child: Row(
        crossAxisAlignment: oneLine
            ? CrossAxisAlignment.center
            : CrossAxisAlignment.start,
        children: <Widget>[
          Padding(
            padding: EdgeInsets.only(top: oneLine ? 0 : 5),
            child: AppStatusDot(
              color: dot,
              pulse: pulse,
              pulseDuration: AppMotion.pulseFast,
              square: tone == AppNoticeTone.neutral && title != null,
            ),
          ),
          const SizedBox(width: AppSpacing.xl),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                if (title != null) ...<Widget>[
                  Text(
                    title!,
                    style: (titleStyle ?? context.type.strong).copyWith(
                      color: colors.text,
                    ),
                  ),
                  const SizedBox(height: 3),
                ],
                DefaultTextStyle(
                  style: context.type.bodySm.copyWith(color: colors.textOnChip),
                  child: body,
                ),
                if (actions != null) ...<Widget>[
                  const SizedBox(height: AppSpacing.xl),
                  Wrap(
                    spacing: AppSpacing.md,
                    runSpacing: AppSpacing.md,
                    children: actions!,
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}
