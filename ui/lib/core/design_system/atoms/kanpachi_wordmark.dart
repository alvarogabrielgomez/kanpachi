import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';

/// El logotipo.
///
/// Un solo archivo monocromo teñido desde los tokens, y no una variante negra
/// y otra blanca. El diseño en HTML alterna dos `<img>` con `display` porque
/// CSS no puede teñir una imagen; acá sí se puede, y dos archivos de color
/// distinto serían dos verdades que el tema no controla — cambiar la paleta
/// dejaría uno de los dos desalineado sin que nadie se entere.
class KanpachiWordmark extends StatelessWidget {
  const KanpachiWordmark({this.height = 14, this.opacity = 1, super.key});

  final double height;

  /// En la barra de título va ligeramente apagado, para que la marca no
  /// compite con el contenido de la pantalla.
  final double opacity;

  /// La relación del `viewBox` original, 202 × 51.
  static const double _aspect = 202 / 51;

  @override
  Widget build(BuildContext context) {
    // El apagado va dentro del tinte que ya se aplica, y no en un `Opacity`
    // envolviéndolo. El SVG es monocromo y se tiñe con `srcIn`, así que un
    // color con alfa da exactamente el mismo píxel, sin la capa fuera de
    // pantalla que `Opacity` obliga a crear. Vive en la barra de título, o
    // sea en lo que se repinta cada vez que algo de la barra cambia.
    return SvgPicture.asset(
      'assets/logo/kanpachi_wordmark.svg',
      height: height,
      width: height * _aspect,
      colorFilter: ColorFilter.mode(
        context.colors.text.withValues(alpha: opacity),
        BlendMode.srcIn,
      ),
      semanticsLabel: 'Kanpachi',
    );
  }
}
