// lib/core/design_system/tokens/color_tokens.dart

import 'package:flutter/material.dart';
import 'package:theme_tailor_annotation/theme_tailor_annotation.dart';

part 'color_tokens.tailor.dart'; // dart run build_runner build

/// El contrato ÚNICO de color del design system.
///
/// **Regla no negociable:** los componentes dependen SÓLO de estos tokens. Un
/// componente que hardcodea un `Color(0x…)` o lee `Colors.*` se sale del
/// sistema: deja de responder al theme y nadie se entera hasta que alguien lo
/// ve en pantalla.
///
/// Nombrar por ROL (`textPrimary`), no por color (`chalkWhite`): el rol
/// sobrevive a un cambio de paleta.
@TailorMixin()
class ColorTokens extends ThemeExtension<ColorTokens>
    with _$ColorTokensTailorMixin {
  const ColorTokens({
    required this.primary,
    required this.onPrimary,
    required this.primarySoft,
    required this.background,
    required this.backgroundDark,
    required this.scrim,
    required this.surface,
    required this.card,
    required this.cardRaised,
    required this.sunk,
    required this.textPrimary,
    required this.textSecondary,
    required this.textTertiary,
    required this.iconInactive,
    required this.border,
    required this.borderStrong,
    required this.borderLow,
    required this.success,
    required this.successSurface,
    required this.warning,
    required this.warningSurface,
    required this.failed,
    required this.failedSurface,
  });

  // ── Acento ────────────────────────────────────────────────────────────────
  /// El acento único: acción, foco, selección.
  @override
  final Color primary;

  /// Tinta sobre [primary]. Resolver por CONTRASTE, no fijar: un primary claro
  /// pide texto oscuro y viceversa.
  @override
  final Color onPrimary;

  /// Acento apagado — fondos y bordes de superficies "soft" del acento.
  @override
  final Color primarySoft;

  // ── Rampa de superficies: canvas → surface → card ─────────────────────────
  // Escalones de 2-3% de lightness. El orden importa: cada uno asume el previo.
  @override
  final Color background;
  @override
  final Color backgroundDark;

  /// Scrim de modales — [backgroundDark] con alpha. Es token y no un
  /// `withValues` de call site: así todos los overlays oscurecen igual.
  @override
  final Color scrim;
  @override
  final Color surface;
  @override
  final Color card;
  @override
  final Color cardRaised;

  /// Bajo-canvas: filas hundidas, placeholders.
  @override
  final Color sunk;

  // ── Tinta — jerarquía de texto ────────────────────────────────────────────
  @override
  final Color textPrimary;
  @override
  final Color textSecondary;
  @override
  final Color textTertiary;
  @override
  final Color iconInactive;

  // ── Bordes — si la profundidad se hace con bordes, esto reemplaza sombras ─
  @override
  final Color border;
  @override
  final Color borderStrong;
  @override
  final Color borderLow;

  // ── Semántica: par TINTA + SUPERFICIE por familia ─────────────────────────
  // Siempre en pares: un `success` de texto y un `successSurface` de fondo, con
  // el contraste ya resuelto entre ellos. Un solo token por familia obliga a
  // cada call site a inventar el otro con alpha, y ahí se pierde el contraste.
  @override
  final Color success;
  @override
  final Color successSurface;
  @override
  final Color warning;
  @override
  final Color warningSurface;
  @override
  final Color failed;
  @override
  final Color failedSurface;
}

/// Los valores default del proyecto. El reference tier vive acá; el resto del
/// código sólo conoce los ROLES de arriba.
abstract final class AppPalette {
  static const ColorTokens tokens = ColorTokens(
    primary: Color(0xFFD94A11),
    onPrimary: Color(0xFF120E0B),
    primarySoft: Color(0xFF3A1A0C),
    background: Color(0xFF141210),
    backgroundDark: Color(0xFF0D0B0A),
    scrim: Color(0xB80D0B0A),
    surface: Color(0xFF1A1815),
    card: Color(0xFF201D1A),
    cardRaised: Color(0xFF272320),
    sunk: Color(0xFF100E0D),
    textPrimary: Color(0xFFEDE6DA),
    textSecondary: Color(0xFFB8AB99),
    textTertiary: Color(0xFF8A7F70),
    iconInactive: Color(0xFF6E655A),
    border: Color(0xFF34302B),
    borderStrong: Color(0xFF4A443C),
    borderLow: Color(0xFF262320),
    success: Color(0xFF7FA36B),
    successSurface: Color(0xFF1E2519),
    warning: Color(0xFFD2A03F),
    warningSurface: Color(0xFF2A2314),
    failed: Color(0xFFC1503F),
    failedSurface: Color(0xFF2B1815),
  );
}
