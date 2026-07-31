// lib/core/design_system/tokens/context_ext.dart

import 'package:flutter/material.dart';

import 'color_tokens.dart';
import 'typography_tokens.dart';

/// Accesos ergonómicos a los tokens desde el `context`.
///
/// Sólo se exponen acá los tokens que viajan en el `ThemeData` (color y
/// tipografía). `Spacing`, `Motion` y `Breakpoints` son `static const` y se
/// importan directo — así conservan el `const` de los widgets que los leen.
extension DesignTokensContext on BuildContext {
  /// **Nunca es null y nunca tira.** El lookup de `ThemeExtension` devuelve
  /// null cuando la superficie no lo registró (un subtree montado fuera del
  /// theme, una app secundaria del mismo codebase); romper con `!` mata la
  /// pantalla. El fallback no es excusa para no registrar el theme, pero un
  /// color default es mejor que un crash.
  ColorTokens get colors =>
      Theme.of(this).extension<ColorTokens>() ?? AppPalette.tokens;

  AppType get type => AppType(
    Theme.of(this).extension<TypographyTokens>() ?? AppTypography.tokens,
    colors,
  );
}
