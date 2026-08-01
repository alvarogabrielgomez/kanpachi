import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';

/// El rótulo en versalitas espaciadas que encabeza cada bloque: CÓDIGO DE
/// SALA, TUS JUEGOS, EN LA SALA · 4.
///
/// Va en monoespaciada y muy espaciado a propósito: no compite con el
/// contenido por la atención, pero deja claro dónde empieza cada cosa en una
/// pantalla que llega a tener cinco bloques a la vez.
class AppKicker extends StatelessWidget {
  const AppKicker(this.text, {this.small = false, super.key});

  final String text;
  final bool small;

  @override
  Widget build(BuildContext context) {
    final type = context.type;
    return Text(
      text.toUpperCase(),
      style: (small ? type.kickerSm : type.kicker)
          .copyWith(color: context.colors.textMuted),
    );
  }
}
