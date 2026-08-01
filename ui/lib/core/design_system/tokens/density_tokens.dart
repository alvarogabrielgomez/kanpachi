import 'package:flutter/material.dart';
import 'package:theme_tailor_annotation/theme_tailor_annotation.dart';

part 'density_tokens.tailor.dart';

/// Las tres densidades del diseño.
///
/// Kanpachi se mira en un monitor de escritorio mientras se juega, muchas veces
/// en una ventana pequeña al lado del juego. Quien la usa así quiere ver la
/// lista de miembros entera sin scrollear; quien la deja a pantalla completa
/// prefiere aire. Es una preferencia real, no un capricho de tematización.
enum AppDensity {
  airy('aireada'),
  balanced('equilibrada'),
  dense('densa');

  const AppDensity(this.label);

  /// Lo que se ve en la barra de prototipo.
  final String label;
}

/// Los tres valores que la densidad mueve, y sólo esos tres.
///
/// Es la única parte del espaciado que viaja por `context`, porque es la única
/// que cambia en runtime. El resto de la escala sigue en [AppSpacing] como
/// `static const` para no costarle el `const` a toda la app: acá el precio se
/// paga a propósito y sólo lo pagan los widgets que de verdad respiran.
@TailorMixin()
class DensityTokens extends ThemeExtension<DensityTokens> with _$DensityTokensTailorMixin {
  const DensityTokens({
    required this.pagePad,
    required this.rowPadVertical,
    required this.rowPadHorizontal,
    required this.gap,
  });

  /// El aire vertical entre el borde de la tarjeta y el contenido.
  @override
  final double pagePad;

  @override
  final double rowPadVertical;
  @override
  final double rowPadHorizontal;

  /// La separación entre bloques apilados dentro de una pantalla.
  @override
  final double gap;

  static const DensityTokens airy = DensityTokens(
    pagePad: 34,
    rowPadVertical: 15,
    rowPadHorizontal: 16,
    gap: 18,
  );

  static const DensityTokens balanced = DensityTokens(
    pagePad: 26,
    rowPadVertical: 11,
    rowPadHorizontal: 14,
    gap: 14,
  );

  static const DensityTokens dense = DensityTokens(
    pagePad: 18,
    rowPadVertical: 7,
    rowPadHorizontal: 12,
    gap: 10,
  );

  static const DensityTokens fallback = balanced;

  static DensityTokens of(AppDensity density) => switch (density) {
        AppDensity.airy => airy,
        AppDensity.balanced => balanced,
        AppDensity.dense => dense,
      };
}

/// Fuera de la clase a propósito: theme_tailor toma por token cada getter que
/// encuentre dentro, y lo metería en `copyWith` y en `lerp` como si fuera un
/// valor independiente. Esto es un derivado de dos tokens que ya existen, y
/// dos fuentes para el mismo dato terminan discrepando.
extension DensityPadding on DensityTokens {
  EdgeInsets get rowPadding => EdgeInsets.symmetric(
        vertical: rowPadVertical,
        horizontal: rowPadHorizontal,
      );
}
