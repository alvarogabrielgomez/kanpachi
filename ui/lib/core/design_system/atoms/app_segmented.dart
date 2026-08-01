import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// Una opción del control segmentado.
class AppSegment<T> {
  const AppSegment({
    required this.value,
    required this.icon,
    required this.tooltip,
  });

  final T value;
  final IconData icon;

  /// Obligatorio: un control que sólo tiene iconos no se explica solo, y aquí
  /// las dos opciones se distinguen por un matiz (rejilla contra lista) que no
  /// todo el mundo lee igual.
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
    super.key,
  });

  final List<AppSegment<T>> segments;
  final T value;
  final ValueChanged<T> onChanged;
  final double itemSize;

  /// Cuando el control tiene que estirarse a lo alto de la fila que lo
  /// acompaña, como al lado del buscador.
  final double? itemWidth;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: EdgeInsets.all(itemWidth == null ? AppSpacing.xxs : 3),
      decoration: BoxDecoration(
        color: context.colors.chip,
        borderRadius: AppRadius.pill,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
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
            width: width ?? size,
            height: size,
            decoration: BoxDecoration(
              color: selected ? colors.accent : Colors.transparent,
              borderRadius: AppRadius.pill,
            ),
            child: Icon(
              segment.icon,
              size: 15,
              color: selected ? colors.accentInk : colors.textMuted,
            ),
          ),
        ),
      ),
    );
  }
}
