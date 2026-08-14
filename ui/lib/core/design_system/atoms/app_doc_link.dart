import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';

/// Un trozo de texto que lleva a una página, subrayado al pasar por encima.
///
/// # Por qué el subrayado llega con el ratón y no está siempre
///
/// Porque estos enlaces viven DENTRO de un párrafo, y un subrayado permanente
/// parte la frase en dos: se lee como un trozo distinto antes de que nadie
/// quiera pulsarlo. En color de acento ya se distingue de la prosa; el
/// subrayado es lo que contesta «esto se pulsa» en el instante en que el puntero
/// dice que alguien se lo está preguntando.
///
/// Iba sin él, y el color solo no alcanzaba: el acento también lo llevan textos
/// que no son enlaces —la línea de la versión nueva, sin ir más lejos—, así que
/// el color no podía significar «pulsable» él solo.
class AppDocLink extends StatefulWidget {
  const AppDocLink({
    required this.label,
    required this.onTap,
    this.style,
    super.key,
  });

  final String label;
  final VoidCallback onTap;

  /// El estilo del párrafo que lo contiene. El color lo pone esta clase, para
  /// que un enlace no pueda acabar del color del texto que lo rodea.
  final TextStyle? style;

  @override
  State<AppDocLink> createState() => _AppDocLinkState();
}

class _AppDocLinkState extends State<AppDocLink> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final Color color = context.colors.accent;
    final TextStyle base = (widget.style ?? context.type.bodySm).copyWith(
      color: color,
      decoration: _hovered ? TextDecoration.underline : null,
      decorationColor: color,
    );
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        // `opaque` por lo mismo que el botón de cuenta: sin esto solo responden
        // los píxeles pintados, y los huecos entre letras no son el enlace.
        behavior: HitTestBehavior.opaque,
        onTap: widget.onTap,
        // El texto no se mueve al subrayarse, así que la animación es del color
        // y nada más. Un `AnimatedDefaultTextStyle` no interpola `decoration`:
        // la línea aparece de golpe igual, y de paso costaría un rebuild por
        // fotograma para no animar nada.
        child: AnimatedDefaultTextStyle(
          duration: AppMotion.hover,
          style: base,
          child: Text(widget.label),
        ),
      ),
    );
  }
}
