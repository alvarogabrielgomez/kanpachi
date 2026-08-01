import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// Una opción del control segmentado: o un icono, o una palabra.
class AppSegment<T> {
  const AppSegment({
    required this.value,
    required this.tooltip,
    this.icon,
    this.label,
  }) : assert(
          (icon == null) != (label == null),
          'Un segmento lleva icono O palabra, nunca los dos ni ninguno',
        );

  final T value;
  final IconData? icon;

  /// La palabra, cuando el dato NO se deja dibujar. El protocolo de un rango
  /// de puertos es TCP, UDP o ambos: no hay icono que diga eso, y con iconos
  /// el dato central de la fila sólo se resolvía leyendo el tooltip.
  final String? label;

  /// Obligatorio incluso con palabra: sigue siendo un control diminuto, y con
  /// icono las dos opciones se distinguen por un matiz (rejilla contra lista)
  /// que no todo el mundo lee igual.
  final String tooltip;
}

/// El conmutador de dos posiciones sobre fondo de chip: portadas o lista.
///
/// Es un control de PRESENTACIÓN, no de datos: cambia cómo se ven los juegos,
/// nunca cuáles. Por eso vive en el design system y no en la feature.
class AppSegmented<T> extends StatelessWidget {
  const AppSegmented({
    required this.segments,
    required this.value,
    required this.onChanged,
    this.itemSize = 26,
    this.itemWidth,
    this.groupPadding = 3,
    super.key,
  });

  final List<AppSegment<T>> segments;
  final T value;
  final ValueChanged<T> onChanged;
  final double itemSize;

  /// Cuando el control tiene que estirarse a lo alto de la fila que lo
  /// acompaña, como al lado del buscador.
  final double? itemWidth;

  /// El aire del chip alrededor de los segmentos. Explícito y no deducido de
  /// `itemWidth`: la heurística que había bajaba el grupo a 2 px en cuanto un
  /// segmento dejaba de tener ancho fijo, que no tiene nada que ver.
  final double groupPadding;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: EdgeInsets.all(groupPadding),
      decoration: BoxDecoration(
        color: context.colors.chip,
        borderRadius: AppRadius.pill,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        // El diseño separa los segmentos 2 px; pegados, el seleccionado toca
        // al de al lado y los dos se leen como una sola píldora larga.
        spacing: AppSpacing.xxs,
        children: <Widget>[
          for (final AppSegment<T> segment in segments)
            _SegmentButton<T>(
              segment: segment,
              selected: segment.value == value,
              size: itemSize,
              width: itemWidth,
              onTap: () => onChanged(segment.value),
            ),
        ],
      ),
    );
  }
}

class _SegmentButton<T> extends StatelessWidget {
  const _SegmentButton({
    required this.segment,
    required this.selected,
    required this.size,
    required this.width,
    required this.onTap,
  });

  final AppSegment<T> segment;
  final bool selected;
  final double size;
  final double? width;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Tooltip(
      message: segment.tooltip,
      child: MouseRegion(
        cursor: SystemMouseCursors.click,
        child: GestureDetector(
          onTap: onTap,
          child: Container(
            // Con palabra no hay ancho fijo: lo pone el texto más su aire, y
            // forzar un cuadrado recortaría "AMBOS".
            width: segment.label == null ? (width ?? size) : null,
            height: size,
            alignment: Alignment.center,
            padding: segment.label == null
                ? null
                : const EdgeInsets.symmetric(horizontal: AppSpacing.xl),
            decoration: BoxDecoration(
              color: selected ? colors.accent : Colors.transparent,
              borderRadius: AppRadius.pill,
            ),
            child: segment.label == null
                ? Icon(
                    segment.icon,
                    size: 15,
                    color: selected ? colors.accentInk : colors.textMuted,
                  )
                : Text(
                    segment.label!,
                    style: context.type.monoXxs.copyWith(
                      fontSize: 11,
                      height: 1,
                      fontWeight: FontWeight.w600,
                      letterSpacing: 0.66,
                      color: selected ? colors.accentInk : colors.textMuted,
                    ),
                  ),
          ),
        ),
      ),
    );
  }
}
