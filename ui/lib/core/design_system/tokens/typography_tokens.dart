import 'package:flutter/material.dart';
import 'package:theme_tailor_annotation/theme_tailor_annotation.dart';

part 'typography_tokens.tailor.dart';

/// Las dos familias de la app.
///
/// La monoespaciada no es decoración: la lleva todo lo que alguien va a leer
/// en voz alta, comparar carácter a carácter o teclear a mano — códigos de
/// sala, IPs, puertos, latencias. En una proporcional, `100.87.3.11` y
/// `100.87.3.ll` se parecen demasiado.
abstract final class AppFonts {
  /// La de la interfaz. En Windows resuelve a Segoe UI.
  static const String sans = 'Segoe UI';

  /// La de los datos literales.
  static const String mono = 'Consolas';

  static const List<String> sansFallback = <String>[
    'Segoe UI Variable',
    'system-ui',
    'Roboto',
  ];

  static const List<String> monoFallback = <String>[
    'Cascadia Mono',
    'Courier New',
  ];
}

/// Los roles tipográficos. Como los colores, se nombran por para qué sirven y
/// no por cómo se ven, para que ajustar la escala no obligue a repasar
/// pantalla por pantalla.
@TailorMixin()
class TypographyTokens extends ThemeExtension<TypographyTokens>
    with _$TypographyTokensTailorMixin {
  const TypographyTokens({
    required this.display,
    required this.titleLg,
    required this.titleMd,
    required this.titleSm,
    required this.titleXs,
    required this.gameName,
    required this.sectionTitle,
    required this.bodyLg,
    required this.body,
    required this.bodySm,
    required this.labelLg,
    required this.label,
    required this.labelSm,
    required this.strong,
    required this.strongSm,
    required this.buttonLg,
    required this.button,
    required this.buttonSm,
    required this.kicker,
    required this.kickerSm,
    required this.mono,
    required this.statusLabel,
    required this.statusMono,
    required this.monoSm,
    required this.monoXs,
    required this.monoXxs,
  });

  /// El título de una pantalla que ocupa toda la ventana: la bienvenida.
  @override
  final TextStyle display;

  /// El nombre de la sala.
  @override
  final TextStyle titleLg;
  @override
  final TextStyle titleMd;
  @override
  final TextStyle titleSm;

  /// Título de diálogo.
  @override
  final TextStyle titleXs;

  /// El nombre del juego activo dentro de la sala.
  @override
  final TextStyle gameName;

  /// "Creando la sala…", "Buscando la sala…".
  @override
  final TextStyle sectionTitle;

  @override
  final TextStyle bodyLg;
  @override
  final TextStyle body;
  @override
  final TextStyle bodySm;

  @override
  final TextStyle labelLg;
  @override
  final TextStyle label;
  @override
  final TextStyle labelSm;

  /// Encabezado de un aviso o de una fila destacada.
  @override
  final TextStyle strong;
  @override
  final TextStyle strongSm;

  @override
  final TextStyle buttonLg;
  @override
  final TextStyle button;
  @override
  final TextStyle buttonSm;

  /// Los rótulos en versalitas espaciadas que encabezan cada bloque:
  /// CÓDIGO DE SALA, TUS JUEGOS, EN LA SALA · 4.
  @override
  final TextStyle kicker;
  @override
  final TextStyle kickerSm;

  /// La barra de estado del pie, sans y mono.
  ///
  /// Es un escalón por debajo de `labelSm`/`monoSm` y con peso normal: la
  /// barra acompaña, no compite. Con la escala de al lado iba un punto más
  /// grande y se leía como contenido.
  @override
  final TextStyle statusLabel;
  @override
  final TextStyle statusMono;

  /// Datos literales: código, dirección, puertos.
  @override
  final TextStyle mono;
  @override
  final TextStyle monoSm;
  @override
  final TextStyle monoXs;
  @override
  final TextStyle monoXxs;
}

abstract final class AppTypography {
  static const TextStyle _sans = TextStyle(
    fontFamily: AppFonts.sans,
    fontFamilyFallback: AppFonts.sansFallback,
  );

  static const TextStyle _mono = TextStyle(
    fontFamily: AppFonts.mono,
    fontFamilyFallback: AppFonts.monoFallback,
  );

  static final TypographyTokens tokens = TypographyTokens(
    display: _sans.copyWith(
      fontSize: 28,
      height: 1.2,
      fontWeight: FontWeight.w700,
      letterSpacing: -0.56,
    ),
    titleLg: _sans.copyWith(
      fontSize: 26,
      height: 1.15,
      fontWeight: FontWeight.w700,
      letterSpacing: -0.52,
    ),
    titleMd: _sans.copyWith(
      fontSize: 22,
      height: 1.2,
      fontWeight: FontWeight.w700,
      letterSpacing: -0.33,
    ),
    titleSm: _sans.copyWith(
      fontSize: 21,
      height: 1.2,
      fontWeight: FontWeight.w700,
      letterSpacing: -0.32,
    ),
    titleXs: _sans.copyWith(
      fontSize: 20,
      height: 1.25,
      fontWeight: FontWeight.w700,
      letterSpacing: -0.3,
    ),
    gameName: _sans.copyWith(
      fontSize: 18,
      height: 1.25,
      fontWeight: FontWeight.w700,
      letterSpacing: -0.18,
    ),
    sectionTitle: _sans.copyWith(
      fontSize: 19,
      height: 1.2,
      fontWeight: FontWeight.w600,
    ),
    bodyLg: _sans.copyWith(fontSize: 15, height: 1.6),
    body: _sans.copyWith(fontSize: 13.5, height: 1.6),
    bodySm: _sans.copyWith(fontSize: 12.5, height: 1.5),
    labelLg: _sans.copyWith(fontSize: 15, height: 1.3, fontWeight: FontWeight.w500),
    label: _sans.copyWith(fontSize: 13.5, height: 1.3, fontWeight: FontWeight.w500),
    labelSm: _sans.copyWith(fontSize: 12.5, height: 1.3, fontWeight: FontWeight.w500),
    strong: _sans.copyWith(fontSize: 14, height: 1.3, fontWeight: FontWeight.w600),
    strongSm: _sans.copyWith(fontSize: 13.5, height: 1.4, fontWeight: FontWeight.w600),
    buttonLg: _sans.copyWith(fontSize: 15, height: 1, fontWeight: FontWeight.w700),
    button: _sans.copyWith(fontSize: 14.5, height: 1, fontWeight: FontWeight.w700),
    buttonSm: _sans.copyWith(fontSize: 13.5, height: 1, fontWeight: FontWeight.w700),
    kicker: _mono.copyWith(
      fontSize: 11.5,
      height: 1,
      fontWeight: FontWeight.w600,
      letterSpacing: 1.61,
    ),
    kickerSm: _mono.copyWith(
      fontSize: 11,
      height: 1,
      fontWeight: FontWeight.w600,
      letterSpacing: 1.54,
    ),
    statusLabel: _sans.copyWith(fontSize: 11.5, height: 1),
    statusMono: _mono.copyWith(fontSize: 11.5, height: 1),
    mono: _mono.copyWith(fontSize: 15, height: 1),
    monoSm: _mono.copyWith(fontSize: 12.5, height: 1),
    monoXs: _mono.copyWith(fontSize: 11, height: 1.3),
    monoXxs: _mono.copyWith(fontSize: 10.5, height: 1.4),
  );
}
