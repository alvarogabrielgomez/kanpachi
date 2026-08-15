import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/returning.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// Entrar a otra sala teniendo una vuelta pendiente.
///
/// # Por qué solo cubre ese caso
///
/// Porque es el único que la ventana puede alcanzar. Estando DENTRO de una sala
/// la pantalla es la de la sala, que no tiene campo de código, así que desde acá
/// no se llega a cambiar de sala teniéndola abierta. Eso está bien y se queda: la
/// versión de terminal sí lo alcanza, y allá la compuerta lo pregunta.
///
/// Quien está volviendo nunca hospeda nada, así que acá no existe la variante de
/// cerrar una sala propia.
///
/// # Va ANTES del diálogo de confianza
///
/// Se pregunta primero por lo que se pierde y después por la máquina a la que se
/// va a hablar. Si alguien va a decir que no, que lo diga antes de haber leído un
/// párrafo sobre un registro que ya no va a usar.
class ConfirmDisplaceDialog extends StatelessWidget {
  const ConfirmDisplaceDialog({
    required this.returning,
    required this.onConfirm,
    super.key,
  });

  /// La sala que se deja de esperar.
  final Returning returning;

  /// Lo que sigue si se confirma. La pantalla que abrió esto es la que sabe
  /// adónde iba, así que el camino no se decide acá.
  final VoidCallback onConfirm;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final String nombre = returning.name.isEmpty
        ? returning.code
        : returning.name;

    return AppModal(
      width: 420,
      onDismiss: shell.closeDialog,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text(
            'Dejar de volver a $nombre',
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            'Estás esperando a volver a esa sala, y esto lo cancela.\n\n'
            'Su código queda guardado igual, así que puedes volver a entrar '
            'cuando quieras. Lo que se apaga es que se intente solo.',
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            cancelLabel: 'Seguir esperando',
            confirmLabel: 'Dejar de esperar',
            onCancel: shell.closeDialog,
            onConfirm: () {
              shell.closeDialog();
              onConfirm();
            },
          ),
        ],
      ),
    );
  }
}
