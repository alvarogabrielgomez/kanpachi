import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/color_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/typography_tokens.dart';

/// El `ThemeData` se arma acá y en ningún otro lado.
///
/// Hace dos cosas y sólo dos: registra los tokens en `extensions` para que
/// `context.colors` los encuentre, y tematiza los widgets de Material una vez.
/// Un `TextField(decoration: ...)` suelto en una pantalla es una decisión de
/// diseño escondida donde nadie la va a buscar cuando haya que cambiarla.
abstract final class AppTheme {
  static ThemeData build({
    required ColorTokens colors,
    required Brightness brightness,
    DensityTokens density = DensityTokens.balanced,
  }) {
    final TypographyTokens type = AppTypography.tokens;

    return ThemeData(
      useMaterial3: true,
      brightness: brightness,
      scaffoldBackgroundColor: colors.background,
      canvasColor: colors.surface,
      // El ColorScheme existe para los widgets de Material que no vamos a
      // reemplazar (el cursor de un campo, el ripple). Los roles propios de
      // Kanpachi viven en ColorTokens; esto sólo evita que Material pinte de
      // morado por defecto en un rincón que se nos escape.
      colorScheme: ColorScheme.fromSeed(
        seedColor: colors.accent,
        brightness: brightness,
        primary: colors.accent,
        onPrimary: colors.accentInk,
        surface: colors.surface,
        onSurface: colors.text,
        error: colors.warn,
      ),
      textTheme: TextTheme(
        displayLarge: type.display,
        titleLarge: type.titleLg,
        titleMedium: type.titleMd,
        bodyLarge: type.bodyLg,
        bodyMedium: type.body,
        bodySmall: type.bodySm,
        labelLarge: type.labelLg,
        labelMedium: type.label,
        labelSmall: type.labelSm,
      ).apply(bodyColor: colors.text, displayColor: colors.text),
      // Sin splash ni highlight de Material: el diseño expresa el "estoy
      // reaccionando" con color de borde y con el relieve del botón, y una
      // onda circular encima de eso lee como dos respuestas a un mismo clic.
      splashFactory: NoSplash.splashFactory,
      highlightColor: Colors.transparent,
      hoverColor: Colors.transparent,
      dividerTheme: DividerThemeData(
        color: colors.border,
        thickness: 1,
        space: 1,
      ),
      textSelectionTheme: TextSelectionThemeData(
        cursorColor: colors.accent,
        selectionColor: colors.accent.withValues(alpha: 0.24),
        selectionHandleColor: colors.accent,
      ),
      // Sin barra de scroll en ningún sitio. Quien la quita de verdad es el
      // `scrollBehavior` de la app, y esto es el cinturón: si alguien pone un
      // `Scrollbar` a mano, que al menos no se pinte.
      scrollbarTheme: const ScrollbarThemeData(
        thickness: WidgetStatePropertyAll<double>(0),
        thumbVisibility: WidgetStatePropertyAll<bool>(false),
      ),
      extensions: <ThemeExtension<dynamic>>[colors, type, density],
    );
  }

  static ThemeData light({DensityTokens density = DensityTokens.balanced}) =>
      build(
        colors: AppPalette.light,
        brightness: Brightness.light,
        density: density,
      );

  static ThemeData dark({DensityTokens density = DensityTokens.balanced}) =>
      build(
        colors: AppPalette.dark,
        brightness: Brightness.dark,
        density: density,
      );
}
