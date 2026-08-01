import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// Confirmar que se abre un juego.
///
/// Es el único momento en que Kanpachi abre puertos, así que es el único que
/// se confirma. El texto cambia según de dónde venga: crear una sala CON juego
/// no es lo mismo que cambiar el de una sala que ya tiene gente dentro, y la
/// diferencia importa porque en el segundo caso el cambio le afecta a otros.
class ConfirmGameDialog extends StatelessWidget {
  const ConfirmGameDialog({
    required this.game,
    required this.insideRoom,
    super.key,
  });

  final Game game;

  /// Ya hay sala. Cambia el título, el cuerpo y el verbo del botón.
  final bool insideRoom;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final SessionCubit session = context.read<SessionCubit>();

    return AppModal(
      onDismiss: () {
        session.cancelProposal();
        shell.closeDialog();
      },
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          AppKicker(
            insideRoom ? 'Abrir juego en la sala' : 'Crear sala con juego',
          ),
          const SizedBox(height: AppSpacing.lg),
          Row(
            children: <Widget>[
              // El hueco de portada sólo cuando se están viendo portadas.
              // Quien eligió la vista de lista dijo que no quiere portadas, y
              // el diálogo no es sitio para discutírselo.
              if (context.select<ShellCubit, GameArtMode>(
                    (ShellCubit c) => c.state.artMode,
                  ) ==
                  GameArtMode.cover) ...<Widget>[
                const AppCover.dialog(),
                const SizedBox(width: AppSpacing.xxl),
              ],
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      game.name,
                      style: context.type.titleSm.copyWith(color: colors.text),
                    ),
                    const SizedBox(height: 5),
                    Text(
                      game.portsLabel,
                      style:
                          context.type.monoSm.copyWith(color: colors.textMuted),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: AppSpacing.x4l),
          AppExplainer(
            insideRoom
                ? 'Se abren esos puertos hacia los miembros de la sala y se '
                    'cierran al cambiar de juego o al salir.'
                : 'Eres el host: el juego corre en tu PC y esos puertos se '
                    'abren solo hacia los miembros de la sala.',
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            confirmLabel: insideRoom ? 'Abrir juego' : 'Crear sala',
            onCancel: () {
              session.cancelProposal();
              shell.closeDialog();
            },
            onConfirm: () {
              shell.closeDialog();
              if (insideRoom) {
                session.applyGame(game);
                shell.go(AppScreen.room);
              } else {
                session.createRoom(name: 'Sala de Kanpachi', game: game);
                shell.go(AppScreen.room);
              }
            },
          ),
        ],
      ),
    );
  }
}

/// Confirmar una expulsión.
///
/// El cuerpo dice lo que casi nadie espera: expulsar NO revoca el código, así
/// que quien salga puede volver si todavía lo tiene. Callarlo dejaría al host
/// creyendo que resolvió algo que no resolvió, y por eso el texto lleva de la
/// mano a renovar el código.
class ConfirmKickDialog extends StatelessWidget {
  const ConfirmKickDialog({required this.member, super.key});

  final Member member;

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
            'Expulsar a ${member.name}',
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            'Sale de la sala ahora mismo.\n\n'
            'Puede volver a entrar si todavía tiene el código. Para que no '
            'vuelva, renueva el código: los que ya están dentro se quedan.',
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            confirmLabel: 'Expulsar',
            onCancel: shell.closeDialog,
            onConfirm: () {
              context.read<SessionCubit>().kick(member);
              shell.closeDialog();
            },
          ),
        ],
      ),
    );
  }
}

/// Confirmar la renovación del código.
class ConfirmRenewDialog extends StatelessWidget {
  const ConfirmRenewDialog({required this.membersInside, super.key});

  final int membersInside;

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
            'Renovar el código de la sala',
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            'El código actual deja de funcionar.\n'
            'Los $membersInside que están dentro siguen dentro.\n\n'
            'Pásales el nuevo solo a quien quieras que pueda volver a entrar.',
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            confirmLabel: 'Renovar',
            onCancel: shell.closeDialog,
            onConfirm: () {
              context.read<SessionCubit>().renewCode();
              shell.closeDialog();
            },
          ),
        ],
      ),
    );
  }
}
