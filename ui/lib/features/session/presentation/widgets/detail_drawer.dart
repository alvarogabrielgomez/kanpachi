import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/progress_steps.dart';

/// El cajón de diagnóstico: los pasos de una operación, y lo que dijo el daemon.
///
/// # Por qué es uno solo para los dos sitios que lo abren
///
/// Porque los dos enseñan lo mismo. El aviso de fallo lo abre sobre la acción
/// que no ocurrió, y el aviso de vuelta sobre el intento que no entró, y en los
/// dos casos lo que hay que leer son los pasos que sí pasaron y la frase cruda
/// con la que terminó. Estaba escrito dentro del aviso de fallo, así que el
/// aviso de vuelta no tenía cómo enseñarlo sin copiar la caja entera.
///
/// El orden no es casual: los pasos van primero y la frase cruda al final. La
/// frase dice dónde se detuvo, los pasos dicen hasta dónde llegó, y casi
/// siempre es lo segundo lo que cuenta cuál de los dos hay que ir a mirar.
class DetailDrawer extends StatelessWidget {
  const DetailDrawer({required this.progress, required this.reason, super.key});

  /// Los pasos, cuando los hay. Null es que nadie los recogió.
  final Progress? progress;

  /// Texto crudo del daemon. **Nunca es el mensaje principal de nada**: está
  /// escrito para diagnosticar, y nombra cosas que ningún jugador ha oído.
  final String reason;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final Progress? p = progress;
    return Container(
      padding: const EdgeInsets.all(AppSpacing.x3l),
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        borderRadius: AppRadius.allLg,
        border: Border.all(color: colors.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          if (p != null) ProgressSteps(progress: p),
          if (p != null && reason.isNotEmpty)
            const SizedBox(height: AppSpacing.x3l),
          if (reason.isNotEmpty) ...<Widget>[
            Text(
              'Lo que dijo el daemon',
              style: context.type.strong.copyWith(color: colors.textMuted),
            ),
            const SizedBox(height: AppSpacing.md),
            SelectableText(
              reason,
              style: context.type.monoSm.copyWith(color: colors.textOnChip),
            ),
          ],
        ],
      ),
    );
  }
}
