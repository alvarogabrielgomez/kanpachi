// lib/core/design_system/tokens/spacing_tokens.dart
//
// Spacing, radios, bordes, motion y breakpoints. TODO `static const`.
//
// **Por qué const y no ThemeExtension** (a diferencia de ColorTokens): un
// ThemeExtension se lee por `context` y por lo tanto NUNCA es const. Eso se
// propaga: cualquier widget que lo lea pierde su constructor `const`, y con él
// la rama de skip total de `Element.updateChild` (`child.widget == newWidget`
// → sin rebuild). El color paga ese costo porque anima con `lerp` y puede
// cambiar en runtime; el spacing no hace ni una ni la otra.

abstract final class Spacing {
  // ── Escala, base 4 ────────────────────────────────────────────────────────
  /// Dentro de un componente: ícono↔texto, inline adyacentes.
  static const double micro = 4;
  static const double xs = 8;

  /// Interno de componente: padding de card, de botón, de fila.
  static const double sm = 12;
  static const double md = 16;

  /// Entre componentes de una sección.
  static const double lg = 24;

  /// Entre secciones.
  static const double xl = 32;

  /// Entre regiones de página.
  static const double xxl = 48;

  /// Margen horizontal contra el borde de pantalla. No cae en la escala a
  /// propósito: es una medida de página, no un gap entre elementos.
  static const double screenX = 20;

  // ── Radios ────────────────────────────────────────────────────────────────
  static const double radiusInput = 0;
  static const double radiusCard = 2;
  static const double radiusSheet = 8;

  /// "Pill" es un token de radio, no un componente aparte.
  static const double radiusPill = 999;

  // ── Bordes ────────────────────────────────────────────────────────────────
  static const double borderHairline = 1;

  // ── Tap target ────────────────────────────────────────────────────────────
  /// El mínimo de Material. Nada tocable por debajo de esto.
  static const double minTapTarget = 48;
}

// lib/core/design_system/tokens/motion_tokens.dart
abstract final class Motion {
  static const Duration fade = Duration(milliseconds: 160);
  static const Duration sheet = Duration(milliseconds: 220);
  static const Duration page = Duration(milliseconds: 260);
}

// lib/core/design_system/tokens/breakpoint_tokens.dart
abstract final class Breakpoints {
  /// El ancho decide el LAYOUT INTERNO del contenido. La presentación
  /// (bottom sheet vs diálogo) la decide la PLATAFORMA — ver responsive.dart.
  static const double compact = 760;
  static const double expanded = 1180;
}
