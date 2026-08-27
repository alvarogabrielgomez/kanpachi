import 'package:flutter/material.dart';
import 'package:theme_tailor_annotation/theme_tailor_annotation.dart';

part 'color_tokens.tailor.dart';

/// Los colores de Kanpachi, por ROL y no por color.
///
/// Nombrar por rol es lo que hace posible que exista un solo juego de
/// componentes para claro y oscuro: el componente pide `surface` o `border`, y
/// el tema decide qué primitivo hay detrás. Un componente que pidiera "beige"
/// tendría que saber en qué tema está, y eso es justo lo que no debe saber.
///
/// Viaja por `context` a propósito: el color cambia en runtime y anima con
/// `lerp`. Spacing y motion no hacen ninguna de las dos cosas, así que viven en
/// `static const` para no costarle el `const` a quien los use.
@TailorMixin()
class ColorTokens extends ThemeExtension<ColorTokens>
    with _$ColorTokensTailorMixin {
  const ColorTokens({
    required this.background,
    required this.surface,
    required this.surfaceSunken,
    required this.chip,
    required this.text,
    required this.textMuted,
    required this.textOnChip,
    required this.accent,
    required this.accentInk,
    required this.accentShadow,
    required this.border,
    required this.shadowMenu,
    required this.ok,
    required this.idle,
    required this.okInk,
    required this.warn,
    required this.danger,
    required this.dangerSurface,
    required this.warnSurface,
    required this.shapeOne,
    required this.shapeTwo,
    required this.shapeThree,
    required this.shapeFour,
    required this.veil,
    required this.shadow,
    required this.scrim,
    required this.sheen,
    required this.shapeOpacity,
  });

  /// El lienzo de la ventana, por detrás de la tarjeta principal.
  @override
  final Color background;

  /// La tarjeta y todo lo que se apoya sobre el lienzo.
  @override
  final Color surface;

  /// Hundido respecto a [surface]: barras de título y estado, campos, cajas
  /// internas. En claro es más oscuro que la tarjeta y en oscuro más oscuro
  /// también, porque "hundido" es un rol, no una dirección de luminancia.
  @override
  final Color surfaceSunken;

  /// Los bloques de texto explicativo y los controles secundarios.
  @override
  final Color chip;

  @override
  final Color text;

  /// Texto secundario: notas, metadatos, kickers.
  @override
  final Color textMuted;

  /// Texto sobre [chip]. Existe aparte de [textMuted] porque el contraste
  /// contra el chip no es el mismo que contra la superficie.
  @override
  final Color textOnChip;

  /// La acción. Un solo acento en toda la app: si dos cosas compiten por ser
  /// la acción principal de una pantalla, es la pantalla la que está mal.
  @override
  final Color accent;

  /// Lo que se escribe ENCIMA del acento.
  @override
  final Color accentInk;

  /// El borde inferior del botón principal, que le da el relieve de tres
  /// dimensiones.
  @override
  final Color accentShadow;

  @override
  final Color border;

  /// La sombra de un menú flotante, más ligera que la de la ventana.
  ///
  /// Igual en los dos temas: es negro al 28%, y lo que la hace legible en
  /// claro y en oscuro es la superficie que tiene debajo, no el color.
  @override
  final Color shadowMenu;

  /// Verde de "esto está bien": el punto del servicio activo, un peer directo.
  @override
  final Color ok;

  /// El gris de un punto APAGADO: nadie por ese camino, un miembro offline,
  /// uno mismo en la lista.
  ///
  /// # Por qué no es [textMuted], que es de donde salía
  ///
  /// Porque son dos trabajos con dos exigencias opuestas. `textMuted` tiene que
  /// LEERSE, así que vive cerca del texto; un punto apagado tiene que
  /// distinguirse del verde de al lado, y para eso tiene que alejarse. Medido
  /// el 2026-08-26 simulando deuteranopia sobre la paleta oscura de entonces:
  /// entre el verde y `textMuted` había una distancia de 6,8 en Lab, o sea el
  /// mismo punto para quien no distingue rojo de verde. Con este gris son 17.
  ///
  /// El punto no es texto, así que su contraste se mide contra el fondo con el
  /// umbral de lo no textual, y lo pasa en los dos temas.
  @override
  final Color idle;

  /// La tinta que va ENCIMA del verde: la etiqueta INSTALADO.
  ///
  /// Blanca en los dos temas, y por eso tiene token propio en vez de reciclar
  /// `surface`: en tema oscuro `surface` es casi negro, y la etiqueta salía
  /// negra sobre verde.
  @override
  final Color okInk;

  /// Ámbar de "mira esto": nunca rojo. Kanpachi avisa de cosas que el usuario
  /// puede arreglar, no de errores fatales, y el rojo pide una urgencia que
  /// estos avisos no tienen.
  ///
  /// **Tirado hacia el rojo y subido de luminosidad el 2026-08-26**, sin llegar
  /// a rojo. El dorado de antes se confundía con el verde para quien no
  /// distingue rojo de verde, y en la ventana oscura ninguno de los dos
  /// destacaba sobre el fondo. Lo vigila `test/dot_contrast_test.dart`, que
  /// simula protanopia y deuteranopia y exige distancia entre los tres.
  @override
  final Color warn;

  @override
  final Color warnSurface;

  /// Rojo de "esto no se hizo": una acción del usuario que FALLÓ.
  ///
  /// Es el acento tirado hacia el rojo y no un rojo de sistema, para que siga
  /// siendo Kanpachi. Y es otro rol distinto de [warn], que ya avisa de cosas
  /// mejorables: mezclarlos haría que "tu firewall está apagado" y "no se pudo
  /// crear la sala" pesaran igual, y el segundo pide un reintento AHORA.
  ///
  /// Se reserva para lo que el usuario pidió y no ocurrió. Nada de fondo, nada
  /// automático: eso es [warn].
  @override
  final Color danger;

  @override
  final Color dangerSurface;

  /// Las cuatro manchas del fondo ambiental.
  @override
  final Color shapeOne;
  @override
  final Color shapeTwo;
  @override
  final Color shapeThree;
  @override
  final Color shapeFour;

  /// El velo que se echa por encima de las manchas para que el texto siga
  /// legible sin apagarlas del todo.
  @override
  final Color veil;

  /// Sombra proyectada de la tarjeta principal y de los diálogos.
  @override
  final Color shadow;

  /// El oscurecido de fondo cuando hay un diálogo abierto.
  @override
  final Color scrim;

  /// El brillo que recorre la barra de progreso.
  ///
  /// Blanco translúcido en los dos temas, como [okInk] y por el mismo motivo:
  /// no va sobre el lienzo sino sobre el ACENTO, que es naranja en claro y en
  /// oscuro. Un brillo que respondiera al tema saldría oscuro sobre naranja en
  /// modo oscuro, o sea una sombra. En oscuro va más bajo porque el acento
  /// también es más claro ahí y el mismo blanco lo lavaría entero.
  @override
  final Color sheen;

  /// Cuánto se dejan ver las manchas del fondo. En oscuro se bajan, porque el
  /// mismo color sobre un lienzo oscuro pesa mucho más.
  @override
  final double shapeOpacity;
}

/// Las dos instancias, y el fallback.
abstract final class AppPalette {
  static const ColorTokens light = ColorTokens(
    background: Color(0xFFF7EFE6),
    surface: Color(0xFFFFFFFF),
    surfaceSunken: Color(0xFFFAF7F2),
    chip: Color(0xFFF4F1EC),
    text: Color(0xFF141210),
    textMuted: Color(0xFF57514A),
    textOnChip: Color(0xFF463F38),
    accent: Color(0xFF9D3915),
    danger: Color(0xFFA8231A),
    dangerSurface: Color(0xFFF9E1DC),
    accentInk: Color(0xFFFFFFFF),
    accentShadow: Color(0xFF6D2810),
    border: Color(0xFFD8D2C8),
    ok: Color(0xFF3D7A2A),
    idle: Color(0xFF7C746A),
    okInk: Color(0xFFFFFFFF),
    shadowMenu: Color(0x47000000),
    warn: Color(0xFFA85312),
    warnSurface: Color(0xFFF7E9C9),
    shapeOne: Color(0xFFF3B98A),
    shapeTwo: Color(0xFFF2D9A8),
    shapeThree: Color(0xFFE8845A),
    shapeFour: Color(0xFFD9C3A0),
    veil: Color(0x57F7EFE6),
    shadow: Color(0x6B3C2414),
    scrim: Color(0x593C2414),
    sheen: Color(0x8CFFFFFF),
    shapeOpacity: 1,
  );

  static const ColorTokens dark = ColorTokens(
    background: Color(0xFF141210),
    surface: Color(0xFF201E1B),
    surfaceSunken: Color(0xFF1A1815),
    chip: Color(0xFF2A2724),
    text: Color(0xFFF0EDE8),
    textMuted: Color(0xFFA19A8F),
    textOnChip: Color(0xFFC9C2B8),
    accent: Color(0xFFE0703F),
    danger: Color(0xFFE8705F),
    dangerSurface: Color(0xFF2E1B18),
    accentInk: Color(0xFF201E1B),
    accentShadow: Color(0xFFA94B26),
    border: Color(0xFF35322D),
    ok: Color(0xFF89D268),
    idle: Color(0xFF746D64),
    okInk: Color(0xFFFFFFFF),
    shadowMenu: Color(0x47000000),
    warn: Color(0xFFF2913C),
    warnSurface: Color(0xFF2C2418),
    shapeOne: Color(0xFFE0703F),
    shapeTwo: Color(0xFFF0C58A),
    shapeThree: Color(0xFF7F4A2E),
    shapeFour: Color(0xFFB4451F),
    veil: Color(0x57141210),
    shadow: Color(0xCC000000),
    scrim: Color(0x990A0908),
    sheen: Color(0x66FFFFFF),
    shapeOpacity: 0.85,
  );

  /// El que se usa cuando `context` no encuentra la extensión. No es excusa
  /// para no registrar el tema: es la diferencia entre un color raro y un
  /// crash en runtime.
  static const ColorTokens fallback = light;
}
