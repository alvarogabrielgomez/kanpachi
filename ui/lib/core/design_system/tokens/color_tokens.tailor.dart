// coverage:ignore-file
// GENERATED CODE - DO NOT MODIFY BY HAND
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'color_tokens.dart';

// **************************************************************************
// TailorAnnotationsGenerator
// **************************************************************************

mixin _$ColorTokensTailorMixin on ThemeExtension<ColorTokens> {
  Color get background;
  Color get surface;
  Color get surfaceSunken;
  Color get chip;
  Color get text;
  Color get textMuted;
  Color get textOnChip;
  Color get accent;
  Color get accentInk;
  Color get accentShadow;
  Color get border;
  Color get shadowMenu;
  Color get ok;
  Color get okInk;
  Color get warn;
  Color get warnSurface;
  Color get danger;
  Color get dangerSurface;
  Color get shapeOne;
  Color get shapeTwo;
  Color get shapeThree;
  Color get shapeFour;
  Color get veil;
  Color get shadow;
  Color get scrim;
  Color get sheen;
  double get shapeOpacity;

  @override
  ColorTokens copyWith({
    Color? background,
    Color? surface,
    Color? surfaceSunken,
    Color? chip,
    Color? text,
    Color? textMuted,
    Color? textOnChip,
    Color? accent,
    Color? accentInk,
    Color? accentShadow,
    Color? border,
    Color? shadowMenu,
    Color? ok,
    Color? okInk,
    Color? warn,
    Color? warnSurface,
    Color? danger,
    Color? dangerSurface,
    Color? shapeOne,
    Color? shapeTwo,
    Color? shapeThree,
    Color? shapeFour,
    Color? veil,
    Color? shadow,
    Color? scrim,
    Color? sheen,
    double? shapeOpacity,
  }) {
    return ColorTokens(
      background: background ?? this.background,
      surface: surface ?? this.surface,
      surfaceSunken: surfaceSunken ?? this.surfaceSunken,
      chip: chip ?? this.chip,
      text: text ?? this.text,
      textMuted: textMuted ?? this.textMuted,
      textOnChip: textOnChip ?? this.textOnChip,
      accent: accent ?? this.accent,
      accentInk: accentInk ?? this.accentInk,
      accentShadow: accentShadow ?? this.accentShadow,
      border: border ?? this.border,
      shadowMenu: shadowMenu ?? this.shadowMenu,
      ok: ok ?? this.ok,
      okInk: okInk ?? this.okInk,
      warn: warn ?? this.warn,
      warnSurface: warnSurface ?? this.warnSurface,
      danger: danger ?? this.danger,
      dangerSurface: dangerSurface ?? this.dangerSurface,
      shapeOne: shapeOne ?? this.shapeOne,
      shapeTwo: shapeTwo ?? this.shapeTwo,
      shapeThree: shapeThree ?? this.shapeThree,
      shapeFour: shapeFour ?? this.shapeFour,
      veil: veil ?? this.veil,
      shadow: shadow ?? this.shadow,
      scrim: scrim ?? this.scrim,
      sheen: sheen ?? this.sheen,
      shapeOpacity: shapeOpacity ?? this.shapeOpacity,
    );
  }

  @override
  ColorTokens lerp(covariant ThemeExtension<ColorTokens>? other, double t) {
    if (other is! ColorTokens) return this as ColorTokens;
    return ColorTokens(
      background: Color.lerp(background, other.background, t)!,
      surface: Color.lerp(surface, other.surface, t)!,
      surfaceSunken: Color.lerp(surfaceSunken, other.surfaceSunken, t)!,
      chip: Color.lerp(chip, other.chip, t)!,
      text: Color.lerp(text, other.text, t)!,
      textMuted: Color.lerp(textMuted, other.textMuted, t)!,
      textOnChip: Color.lerp(textOnChip, other.textOnChip, t)!,
      accent: Color.lerp(accent, other.accent, t)!,
      accentInk: Color.lerp(accentInk, other.accentInk, t)!,
      accentShadow: Color.lerp(accentShadow, other.accentShadow, t)!,
      border: Color.lerp(border, other.border, t)!,
      shadowMenu: Color.lerp(shadowMenu, other.shadowMenu, t)!,
      ok: Color.lerp(ok, other.ok, t)!,
      okInk: Color.lerp(okInk, other.okInk, t)!,
      warn: Color.lerp(warn, other.warn, t)!,
      warnSurface: Color.lerp(warnSurface, other.warnSurface, t)!,
      danger: Color.lerp(danger, other.danger, t)!,
      dangerSurface: Color.lerp(dangerSurface, other.dangerSurface, t)!,
      shapeOne: Color.lerp(shapeOne, other.shapeOne, t)!,
      shapeTwo: Color.lerp(shapeTwo, other.shapeTwo, t)!,
      shapeThree: Color.lerp(shapeThree, other.shapeThree, t)!,
      shapeFour: Color.lerp(shapeFour, other.shapeFour, t)!,
      veil: Color.lerp(veil, other.veil, t)!,
      shadow: Color.lerp(shadow, other.shadow, t)!,
      scrim: Color.lerp(scrim, other.scrim, t)!,
      sheen: Color.lerp(sheen, other.sheen, t)!,
      shapeOpacity: t < 0.5 ? shapeOpacity : other.shapeOpacity,
    );
  }

  @override
  bool operator ==(Object other) {
    return identical(this, other) ||
        (other.runtimeType == runtimeType &&
            other is ColorTokens &&
            const DeepCollectionEquality().equals(
              background,
              other.background,
            ) &&
            const DeepCollectionEquality().equals(surface, other.surface) &&
            const DeepCollectionEquality().equals(
              surfaceSunken,
              other.surfaceSunken,
            ) &&
            const DeepCollectionEquality().equals(chip, other.chip) &&
            const DeepCollectionEquality().equals(text, other.text) &&
            const DeepCollectionEquality().equals(textMuted, other.textMuted) &&
            const DeepCollectionEquality().equals(
              textOnChip,
              other.textOnChip,
            ) &&
            const DeepCollectionEquality().equals(accent, other.accent) &&
            const DeepCollectionEquality().equals(accentInk, other.accentInk) &&
            const DeepCollectionEquality().equals(
              accentShadow,
              other.accentShadow,
            ) &&
            const DeepCollectionEquality().equals(border, other.border) &&
            const DeepCollectionEquality().equals(
              shadowMenu,
              other.shadowMenu,
            ) &&
            const DeepCollectionEquality().equals(ok, other.ok) &&
            const DeepCollectionEquality().equals(okInk, other.okInk) &&
            const DeepCollectionEquality().equals(warn, other.warn) &&
            const DeepCollectionEquality().equals(
              warnSurface,
              other.warnSurface,
            ) &&
            const DeepCollectionEquality().equals(danger, other.danger) &&
            const DeepCollectionEquality().equals(
              dangerSurface,
              other.dangerSurface,
            ) &&
            const DeepCollectionEquality().equals(shapeOne, other.shapeOne) &&
            const DeepCollectionEquality().equals(shapeTwo, other.shapeTwo) &&
            const DeepCollectionEquality().equals(
              shapeThree,
              other.shapeThree,
            ) &&
            const DeepCollectionEquality().equals(shapeFour, other.shapeFour) &&
            const DeepCollectionEquality().equals(veil, other.veil) &&
            const DeepCollectionEquality().equals(shadow, other.shadow) &&
            const DeepCollectionEquality().equals(scrim, other.scrim) &&
            const DeepCollectionEquality().equals(sheen, other.sheen) &&
            const DeepCollectionEquality().equals(
              shapeOpacity,
              other.shapeOpacity,
            ));
  }

  @override
  int get hashCode {
    return Object.hashAll([
      runtimeType.hashCode,
      const DeepCollectionEquality().hash(background),
      const DeepCollectionEquality().hash(surface),
      const DeepCollectionEquality().hash(surfaceSunken),
      const DeepCollectionEquality().hash(chip),
      const DeepCollectionEquality().hash(text),
      const DeepCollectionEquality().hash(textMuted),
      const DeepCollectionEquality().hash(textOnChip),
      const DeepCollectionEquality().hash(accent),
      const DeepCollectionEquality().hash(accentInk),
      const DeepCollectionEquality().hash(accentShadow),
      const DeepCollectionEquality().hash(border),
      const DeepCollectionEquality().hash(shadowMenu),
      const DeepCollectionEquality().hash(ok),
      const DeepCollectionEquality().hash(okInk),
      const DeepCollectionEquality().hash(warn),
      const DeepCollectionEquality().hash(warnSurface),
      const DeepCollectionEquality().hash(danger),
      const DeepCollectionEquality().hash(dangerSurface),
      const DeepCollectionEquality().hash(shapeOne),
      const DeepCollectionEquality().hash(shapeTwo),
      const DeepCollectionEquality().hash(shapeThree),
      const DeepCollectionEquality().hash(shapeFour),
      const DeepCollectionEquality().hash(veil),
      const DeepCollectionEquality().hash(shadow),
      const DeepCollectionEquality().hash(scrim),
      const DeepCollectionEquality().hash(sheen),
      const DeepCollectionEquality().hash(shapeOpacity),
    ]);
  }
}

extension ColorTokensBuildContextProps on BuildContext {
  ColorTokens get colorTokens => Theme.of(this).extension<ColorTokens>()!;

  /// El lienzo de la ventana, por detrás de la tarjeta principal.
  Color get background => colorTokens.background;

  /// La tarjeta y todo lo que se apoya sobre el lienzo.
  Color get surface => colorTokens.surface;

  /// Hundido respecto a [surface]: barras de título y estado, campos, cajas
  /// internas. En claro es más oscuro que la tarjeta y en oscuro más oscuro
  /// también, porque "hundido" es un rol, no una dirección de luminancia.
  Color get surfaceSunken => colorTokens.surfaceSunken;

  /// Los bloques de texto explicativo y los controles secundarios.
  Color get chip => colorTokens.chip;
  Color get text => colorTokens.text;

  /// Texto secundario: notas, metadatos, kickers.
  Color get textMuted => colorTokens.textMuted;

  /// Texto sobre [chip]. Existe aparte de [textMuted] porque el contraste
  /// contra el chip no es el mismo que contra la superficie.
  Color get textOnChip => colorTokens.textOnChip;

  /// La acción. Un solo acento en toda la app: si dos cosas compiten por ser
  /// la acción principal de una pantalla, es la pantalla la que está mal.
  Color get accent => colorTokens.accent;

  /// Lo que se escribe ENCIMA del acento.
  Color get accentInk => colorTokens.accentInk;

  /// El borde inferior del botón principal, que le da el relieve de tres
  /// dimensiones.
  Color get accentShadow => colorTokens.accentShadow;
  Color get border => colorTokens.border;

  /// La sombra de un menú flotante, más ligera que la de la ventana.
  ///
  /// Igual en los dos temas: es negro al 28%, y lo que la hace legible en
  /// claro y en oscuro es la superficie que tiene debajo, no el color.
  Color get shadowMenu => colorTokens.shadowMenu;

  /// Verde de "esto está bien": el punto del servicio activo, un peer directo.
  Color get ok => colorTokens.ok;

  /// La tinta que va ENCIMA del verde: la etiqueta INSTALADO.
  ///
  /// Blanca en los dos temas, y por eso tiene token propio en vez de reciclar
  /// `surface`: en tema oscuro `surface` es casi negro, y la etiqueta salía
  /// negra sobre verde.
  Color get okInk => colorTokens.okInk;

  /// Ámbar de "mira esto": nunca rojo. Kanpachi avisa de cosas que el usuario
  /// puede arreglar, no de errores fatales, y el rojo pide una urgencia que
  /// estos avisos no tienen.
  Color get warn => colorTokens.warn;
  Color get warnSurface => colorTokens.warnSurface;

  /// Rojo de "esto no se hizo": una acción del usuario que FALLÓ.
  ///
  /// Es el acento tirado hacia el rojo y no un rojo de sistema, para que siga
  /// siendo Kanpachi. Y es otro rol distinto de [warn], que ya avisa de cosas
  /// mejorables: mezclarlos haría que "tu firewall está apagado" y "no se pudo
  /// crear la sala" pesaran igual, y el segundo pide un reintento AHORA.
  ///
  /// Se reserva para lo que el usuario pidió y no ocurrió. Nada de fondo, nada
  /// automático: eso es [warn].
  Color get danger => colorTokens.danger;
  Color get dangerSurface => colorTokens.dangerSurface;

  /// Las cuatro manchas del fondo ambiental.
  Color get shapeOne => colorTokens.shapeOne;
  Color get shapeTwo => colorTokens.shapeTwo;
  Color get shapeThree => colorTokens.shapeThree;
  Color get shapeFour => colorTokens.shapeFour;

  /// El velo que se echa por encima de las manchas para que el texto siga
  /// legible sin apagarlas del todo.
  Color get veil => colorTokens.veil;

  /// Sombra proyectada de la tarjeta principal y de los diálogos.
  Color get shadow => colorTokens.shadow;

  /// El oscurecido de fondo cuando hay un diálogo abierto.
  Color get scrim => colorTokens.scrim;

  /// El brillo que recorre la barra de progreso.
  ///
  /// Blanco translúcido en los dos temas, como [okInk] y por el mismo motivo:
  /// no va sobre el lienzo sino sobre el ACENTO, que es naranja en claro y en
  /// oscuro. Un brillo que respondiera al tema saldría oscuro sobre naranja en
  /// modo oscuro, o sea una sombra. En oscuro va más bajo porque el acento
  /// también es más claro ahí y el mismo blanco lo lavaría entero.
  Color get sheen => colorTokens.sheen;

  /// Cuánto se dejan ver las manchas del fondo. En oscuro se bajan, porque el
  /// mismo color sobre un lienzo oscuro pesa mucho más.
  double get shapeOpacity => colorTokens.shapeOpacity;
}
