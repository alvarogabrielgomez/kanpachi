// lib/core/design_system/tokens/typography_tokens.dart

import 'package:flutter/material.dart';
import 'package:theme_tailor_annotation/theme_tailor_annotation.dart';

import 'color_tokens.dart';

part 'typography_tokens.tailor.dart';

/// Las FAMILIAS. Viajan en el ThemeData porque pueden cambiar por superficie.
///
/// Las fuentes se declaran en el pubspec y se referencian por nombre: nada de
/// `GoogleFonts.*` (descarga en runtime, no funciona offline, y se salta el
/// sistema de tokens).
@TailorMixin()
class TypographyTokens extends ThemeExtension<TypographyTokens>
    with _$TypographyTokensTailorMixin {
  const TypographyTokens({
    required this.displayFamily,
    required this.bodyFamily,
    required this.monoFamily,
  });

  @override
  final String displayFamily;
  @override
  final String bodyFamily;
  @override
  final String monoFamily;
}

abstract final class AppTypography {
  static const TypographyTokens tokens = TypographyTokens(
    displayFamily: 'Oswald',
    bodyFamily: 'Inter',
    monoFamily: 'JetBrainsMono',
  );
}

/// El COMPOSITOR: convierte familias + colores en `TextStyle`s listos.
///
/// Se accede por `context.type.display(size: 20)`. Que sea un compositor y no
/// una lista de estilos fijos evita el catálogo de 40 constantes
/// (`titleLargeBoldMuted`…) que nadie recuerda.
class AppType {
  const AppType(this._fonts, this._colors);

  final TypographyTokens _fonts;
  final ColorTokens _colors;

  /// Titulares, tabs, botones.
  ///
  /// [trackEm] = letter-spacing en **em**, que es el eje correcto para
  /// tracking: a 36px un tracking de 1px no se nota; a 11px grita. [tracking]
  /// lo pisa con un absoluto cuando un call site necesita un número exacto.
  TextStyle display({
    double size = 18,
    Color? color,
    FontWeight weight = FontWeight.w600,
    double? trackEm,
    double? tracking,
    double? height,
    bool tabular = false,
  }) => TextStyle(
    fontFamily: _fonts.displayFamily,
    fontSize: size,
    color: color ?? _colors.textPrimary,
    fontWeight: weight,
    letterSpacing: tracking ?? size * (trackEm ?? _opticalTrack(size)),
    height: height,
    fontFeatures: tabular ? const [FontFeature.tabularFigures()] : null,
  );

  TextStyle body({
    double size = 14,
    Color? color,
    FontWeight weight = FontWeight.w400,
    double? tracking,
    double? height,
  }) => TextStyle(
    fontFamily: _fonts.bodyFamily,
    fontSize: size,
    color: color ?? _colors.textSecondary,
    fontWeight: weight,
    letterSpacing: tracking,
    height: height,
  );

  /// Números que tienen que alinearse en columna (timers, tablas, readouts).
  TextStyle mono({double size = 13, Color? color, FontWeight weight = FontWeight.w500}) =>
      TextStyle(
        fontFamily: _fonts.monoFamily,
        fontSize: size,
        color: color ?? _colors.textPrimary,
        fontWeight: weight,
        fontFeatures: const [FontFeature.tabularFigures()],
      );

  /// Tracking óptico: modesto y decreciente con el tamaño. Un default fijo
  /// alto convierte en cartel espaciado a todo lo que olvide pasar tracking.
  double _opticalTrack(double size) {
    if (size >= 40) return 0.01;
    if (size >= 24) return 0.02;
    return 0.04;
  }
}
