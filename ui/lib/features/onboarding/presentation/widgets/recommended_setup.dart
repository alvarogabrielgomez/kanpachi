import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_checkbox.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// La fila del alta que ofrece dejar la máquina como se recomienda.
///
/// # Por qué es una casilla y no un interruptor
///
/// Porque no enseña un estado del sistema: enseña con qué intención se va a
/// pulsar el botón de abajo. El interruptor de Configuración se dibuja de la
/// MEDICIÓN de las reglas y puede cambiar bajo los pies del usuario; esto no
/// mide nada porque todavía no hay nada que medir. Ver [AppCheckbox].
///
/// # Qué pasa si se desmarca
///
/// Nada, y eso es deliberado: desmarcarla NO es decir que no. La decisión de
/// la cuarentena queda **sin tomar**, que es su tercer estado, y la primera
/// sala que se abra desde la terminal preguntará. Decir que no de verdad es
/// apagar el interruptor en Configuración, que además avisa de lo que abre.
class RecommendedSetupRow extends StatelessWidget {
  const RecommendedSetupRow({
    required this.value,
    required this.onChanged,
    super.key,
  });

  final bool value;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Container(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.x5l,
        vertical: AppSpacing.xl,
      ),
      decoration: BoxDecoration(
        color: colors.chip,
        borderRadius: AppRadius.pill,
      ),
      // La fila ocupa el ancho de la columna y el texto es el que cede: con
      // `MainAxisSize.min` y un texto suelto se desbordaba 152 píxeles en la
      // ventana mínima, que es justo lo que el candado de layout mide.
      child: Row(
        children: <Widget>[
          AppCheckbox(value: value, onChanged: onChanged),
          const SizedBox(width: AppSpacing.xl),
          // El texto también alterna: una etiqueta al lado de una casilla es
          // parte de la casilla en cualquier sistema operativo, y obligar a
          // apuntar al cuadrado de 22 píxeles es pedir puntería para nada.
          Expanded(
            child: MouseRegion(
              cursor: SystemMouseCursors.click,
              child: GestureDetector(
                onTap: () => onChanged(!value),
                child: Text(
                  'Aplicar configuración recomendada',
                  style: context.type.strong.copyWith(color: colors.text),
                ),
              ),
            ),
          ),
          const SizedBox(width: AppSpacing.lg),
          AppIconButton(
            icon: Icons.question_mark_rounded,
            tooltip: 'Qué se aplica',
            width: 26,
            height: 26,
            iconSize: 12,
            outlined: true,
            onPressed: () => context.read<ShellCubit>().showDialog(
              AppDialog.recommendedSetup,
            ),
          ),
        ],
      ),
    );
  }
}

/// Lo que la casilla de arriba hace, dicho entero antes de hacerlo.
///
/// **El copy dice la verdad medida y no la cómoda.** La cuarentena de base no
/// es «del túnel» ni dura «mientras juegas»: cierra esos puertos en TODAS las
/// redes de la máquina y se queda puesta hasta que la persona la apague. Decir
/// lo otro es lo que este producto acaba de sacar de tres documentos, y un
/// texto que promete menos de lo que hace deja al usuario sin entender por qué
/// dejó de poder compartir una carpeta.
class RecommendedSetupDialog extends StatelessWidget {
  const RecommendedSetupDialog({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    return AppModal(
      onDismiss: shell.closeDialog,
      // Un solo botón y no [AppModalActions]: no hay nada que cancelar, esto
      // explica y se cierra. La casilla se marca o se desmarca en la pantalla
      // de atrás, que es donde estaba la decisión.
      footer: AppButton(
        label: 'Entendido',
        height: 39.5,
        onPressed: shell.closeDialog,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text(
            'Configuración recomendada',
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            'Al continuar, esta PC deja de responder cuando le piden carpetas '
            'compartidas, Escritorio remoto o administración remota.',
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.x3l),
          const _Punto(
            title: 'Vale en todas tus redes',
            body:
                'Te protege en el wifi de un bar o de un hotel, que es donde '
                'Kanpachi no está mirando. Tu sala ya estaba tapada sin esto.',
          ),
          const SizedBox(height: AppSpacing.xl),
          const _Punto(
            title: 'Se queda hasta que lo desactives',
            body:
                'No se apaga al cerrar la sala ni Kanpachi. Está en '
                'Configuración.',
          ),
          const SizedBox(height: AppSpacing.xl),
          const _Punto(
            title: 'Lo que NO cambia',
            body:
                'Llegar a OTRAS máquinas, a su escritorio o a sus carpetas, '
                'y navegar siguen igual. Desmarca la casilla si compartes '
                'carpetas desde esta PC.',
          ),
        ],
      ),
      // Un solo botón y no [AppModalActions]: no hay nada que cancelar, esto
      // explica y se cierra. La casilla se marca o se desmarca en la pantalla
      // de atrás, que es donde estaba la decisión.
    );
  }
}

/// Una de las cosas que se aplican, con su tilde delante.
class _Punto extends StatelessWidget {
  const _Punto({required this.title, required this.body});

  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: Icon(Icons.check_rounded, size: 15, color: colors.accent),
        ),
        const SizedBox(width: AppSpacing.lg),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                title,
                style: context.type.strong.copyWith(color: colors.text),
              ),
              const SizedBox(height: AppSpacing.xs),
              Text(
                body,
                style: context.type.bodySm.copyWith(color: colors.textOnChip),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
