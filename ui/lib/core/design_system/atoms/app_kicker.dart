import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';

/// Los tres tamaños de rótulo.
enum AppKickerSize {
  /// Encabeza un bloque de la pantalla: CÓDIGO DE SALA, TUS JUEGOS.
  md,

  /// Dentro de una tarjeta o un panel: ENTRAS COMO, NOMBRE DEL JUEGO.
  sm,

  /// Pegado encima del dato que rotula, dentro de su misma caja: PÁSALES ESTA
  /// DIRECCIÓN. Ahí compite con el dato, no con la pantalla, así que baja de
  /// tamaño y de espaciado.
  xs,
}

/// El rótulo en versalitas espaciadas que encabeza cada bloque: CÓDIGO DE
/// SALA, TUS JUEGOS, EN LA SALA · 4.
///
/// Va en monoespaciada y muy espaciado a propósito: no compite con el
/// contenido por la atención, pero deja claro dónde empieza cada cosa en una
/// pantalla que llega a tener cinco bloques a la vez.
class AppKicker extends StatelessWidget {
  const AppKicker(
    this.text, {
    this.small = false,
    this.size,
    this.tracking,
    super.key,
  });

  final String text;

  /// El atajo de siempre para [AppKickerSize.sm]. Se queda porque lo usan seis
  /// pantallas y renombrarlo no arregla nada.
  final bool small;

  /// Gana sobre [small] cuando viene.
  final AppKickerSize? size;

  /// El espaciado entre letras, para el caso suelto. La tarjeta de invitación
  /// es la única que se sale del de su escalón.
  final double? tracking;

  @override
  Widget build(BuildContext context) {
    final type = context.type;
    final AppKickerSize escala =
        size ?? (small ? AppKickerSize.sm : AppKickerSize.md);
    final TextStyle base = switch (escala) {
      AppKickerSize.md => type.kicker,
      AppKickerSize.sm => type.kickerSm,
      AppKickerSize.xs => type.kickerXs,
    };
    return Text(
      text.toUpperCase(),
      // Una línea y sin partir: el diseño los pone con `white-space:nowrap`, y
      // un rótulo partido en dos deja de leerse como rótulo.
      maxLines: 1,
      softWrap: false,
      overflow: TextOverflow.ellipsis,
      style: base.copyWith(
        color: context.colors.textMuted,
        letterSpacing: tracking,
      ),
    );
  }
}
