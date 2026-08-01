import 'package:flutter/material.dart';

import 'package:kanpachi_ui/core/design_system/tokens/color_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/typography_tokens.dart';

/// El único camino para leer tokens desde un widget.
///
/// Nunca `!`. El lookup devuelve null cuando el subtree quedó fuera del tema —
/// una ventana secundaria, un test que monta un widget suelto — y un `!` ahí
/// convierte un color raro en un crash. El fallback no es excusa para no
/// registrar el tema; es la diferencia entre degradar y morir.
extension DesignTokensContext on BuildContext {
  ColorTokens get colors =>
      Theme.of(this).extension<ColorTokens>() ?? AppPalette.fallback;

  TypographyTokens get type =>
      Theme.of(this).extension<TypographyTokens>() ?? AppTypography.tokens;

  DensityTokens get density =>
      Theme.of(this).extension<DensityTokens>() ?? DensityTokens.fallback;
}
