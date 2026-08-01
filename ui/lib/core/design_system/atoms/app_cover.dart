import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// El hueco de la portada de un juego.
///
/// Sale con borde discontinuo porque es exactamente eso: un hueco. Kanpachi no
/// trae portadas embebidas — pesarían más que la app entera y habría que
/// mantenerlas — así que se descargan de SteamDB cuando hay red y hasta
/// entonces se ve el hueco. Un rectángulo gris liso se leería como un juego
/// sin portada; el borde discontinuo dice "falta", no "no hay".
class AppCover extends StatelessWidget {
  const AppCover({
    required this.width,
    required this.height,
    this.badge,
    super.key,
  });

  const AppCover.thumb({super.key, this.badge})
      : width = 34,
        height = 46;

  const AppCover.grid({super.key, this.badge})
      : width = double.infinity,
        height = 104;

  const AppCover.room({super.key, this.badge})
      : width = 44,
        height = 60;

  const AppCover.dialog({super.key, this.badge})
      : width = 52,
        height = 70;

  final double width;
  final double height;

  /// La etiqueta que se superpone arriba a la izquierda: INSTALADO.
  final Widget? badge;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    // La miniatura de 46 px es la única por debajo de 50, y es la que el
    // diseño trata distinto: radio más cerrado, dos palabras cortas y menos
    // tracking. En un hueco de ese tamaño "PORTADA STEAMDB" no cabe sin
    // apretarse contra los bordes.
    final bool mini = height < 50;
    final Widget box = SizedBox(
      width: width,
      height: height,
      child: AppCard(
        dashed: true,
        // Con relleno hundido: las seis portadas del diseño lo llevan. Sin él
        // el hueco se confunde con el fondo de la tarjeta que lo contiene.
        filled: true,
        radius: mini ? AppRadius.allXs : AppRadius.allSm,
        child: Center(
          child: Text(
            mini ? 'STEAM\nDB' : 'PORTADA\nSTEAMDB',
            textAlign: TextAlign.center,
            style: context.type.monoXxs.copyWith(
              color: colors.textMuted,
              fontSize: mini ? 7 : 8.5,
              height: mini ? 1.2 : 1.4,
              letterSpacing: mini ? 0.28 : 0.5,
            ),
          ),
        ),
      ),
    );
    if (badge == null) return box;
    return Stack(
      children: <Widget>[
        box,
        Positioned(top: AppSpacing.sm, left: AppSpacing.sm, child: badge!),
      ],
    );
  }
}

/// La etiqueta verde de INSTALADO sobre una portada.
class AppInstalledBadge extends StatelessWidget {
  const AppInstalledBadge({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
      decoration: BoxDecoration(
        color: colors.ok,
        borderRadius: AppRadius.pill,
      ),
      child: Text(
        'INSTALADO',
        style: context.type.monoXxs.copyWith(
          color: colors.okInk,
          fontSize: 8,
          fontWeight: FontWeight.w600,
          letterSpacing: 0.48,
        ),
      ),
    );
  }
}
