import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// Confirmar que se apaga la cuarentena de base.
///
/// Apagar SÍ se confirma y encender no, y la asimetría es deliberada: poner
/// protección no necesita permiso, quitarla es lo que conviene leer una vez.
/// El texto dice la verdad sin prometer de más, con la forma de la de
/// expulsar: qué vuelve a pasar, y qué NO cambia.
class ConfirmQuarantineOffDialog extends StatelessWidget {
  const ConfirmQuarantineOffDialog({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();

    return AppModal(
      width: 420,
      onDismiss: shell.closeDialog,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text(
            'Reabrir estos puertos',
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            'Tu PC vuelve a responder a compartir archivos y a Escritorio '
            'remoto en todas tus redes. Tu sala de Kanpachi no cambia.',
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            confirmLabel: 'Reabrirlos',
            onCancel: shell.closeDialog,
            onConfirm: () {
              context.read<SessionCubit>().setQuarantine(enabled: false);
              shell.closeDialog();
            },
          ),
        ],
      ),
    );
  }
}
