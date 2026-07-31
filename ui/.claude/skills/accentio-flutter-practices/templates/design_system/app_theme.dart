// lib/core/design_system/theme/app_theme.dart

import 'package:flutter/material.dart';

import '../tokens/color_tokens.dart';
import '../tokens/spacing_tokens.dart';
import '../tokens/typography_tokens.dart';

/// Construye el `ThemeData` a partir de un set de tokens.
///
/// Dos trabajos, y sólo dos:
/// 1. **Registrar los tokens** en `extensions:` para que `context.colors` /
///    `context.type` los encuentren.
/// 2. **Tematizar los widgets de Material** una vez, acá — nunca inline en un
///    call site. Un `ElevatedButton(style: ...)` suelto en una pantalla es una
///    decisión de diseño escondida donde nadie la busca.
abstract final class AppTheme {
  static ThemeData build({
    ColorTokens colors = AppPalette.tokens,
    TypographyTokens fonts = AppTypography.tokens,
    Brightness brightness = Brightness.dark,
  }) {
    final scheme = ColorScheme.fromSeed(
      seedColor: colors.primary,
      brightness: brightness,
    ).copyWith(
      primary: colors.primary,
      onPrimary: colors.onPrimary,
      surface: colors.surface,
      error: colors.failed,
    );

    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      colorScheme: scheme,
      scaffoldBackgroundColor: colors.background,
      fontFamily: fonts.bodyFamily,
      extensions: <ThemeExtension<dynamic>>[colors, fonts],

      // El default de los botones vive acá. Los átomos del DS componen sobre
      // esto en vez de pintar cada uno lo suyo.
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: colors.primary,
          foregroundColor: colors.onPrimary,
          minimumSize: const Size(0, Spacing.minTapTarget),
          shape: const RoundedRectangleBorder(
            borderRadius: BorderRadius.all(Radius.circular(Spacing.radiusInput)),
          ),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: colors.textPrimary,
          side: BorderSide(color: colors.border, width: Spacing.borderHairline),
          minimumSize: const Size(0, Spacing.minTapTarget),
          shape: const RoundedRectangleBorder(
            borderRadius: BorderRadius.all(Radius.circular(Spacing.radiusInput)),
          ),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: colors.sunk,
        border: OutlineInputBorder(
          borderRadius: const BorderRadius.all(
            Radius.circular(Spacing.radiusInput),
          ),
          borderSide: BorderSide(color: colors.border),
        ),
      ),
      dividerTheme: DividerThemeData(
        color: colors.borderLow,
        thickness: Spacing.borderHairline,
        space: Spacing.borderHairline,
      ),
    );
  }
}
