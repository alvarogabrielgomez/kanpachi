import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/seed/presentation/ask_to_host.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_room.dart';
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
                      style: context.type.monoSm.copyWith(
                        color: colors.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: AppSpacing.x4l),
          AppExplainer(
            compact: true,
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
            // Creating waits before navigating; applying does not need to,
            // because there is already a room to come back to. See
            // `SessionCubit.createRoom`.
            onConfirm: () async {
              if (insideRoom) {
                shell.closeDialog();
                shell.go(AppScreen.room);
                await session.applyGame(game);
                return;
              }
              // Crear con juego elegido pasa por la MISMA confianza que crear
              // sin él. Sin esto quedaba un camino a abrir sala que no enseñaba
              // el registro, y sería el que más se usa: el botón grande de la
              // portada es este.
              //
              // El juego elegido sobrevive porque vive en la sesión, no acá:
              // `askToHost` no lo toca y el diálogo de confianza lo recoge de
              // ahí. Por eso este camino NO cierra el diálogo antes de tiempo,
              // `askTrust` lo reemplaza.
              await askToHost(context, suggestedName: 'Sala de Kanpachi');
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
/// Cerrar la sala siendo el host, con gente dentro.
///
/// # Por qué esta sí pregunta
///
/// Porque no es salirse: es terminar la partida de todos los que están dentro.
/// Es la única acción de la app que le cambia el estado a otras máquinas sin
/// que ellas hagan nada, y la diferencia entre "me voy" y "se acabó" no cabe en
/// el nombre de un botón.
///
/// El texto enumera lo que se cierra en vez de decir "se cerrará la sala",
/// porque lo primero se puede comprobar y lo segundo hay que creérselo.
class ConfirmCloseDialog extends StatelessWidget {
  const ConfirmCloseDialog({required this.membersInside, super.key});

  /// Cuántos hay dentro, contándote a ti.
  final int membersInside;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final int otros = membersInside - 1;

    return AppModal(
      width: 430,
      onDismiss: shell.closeDialog,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text(
            'Cerrar la sala',
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            otros == 1
                ? 'Hay 1 persona más dentro y se le va a cortar la partida.'
                : 'Hay $otros personas más dentro y se les va a cortar la '
                      'partida.',
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            'Se avisa a cada uno, se cierran los puertos del juego, se '
            'devuelven las reglas que se hubieran desactivado y se baja la red '
            'de la sala. El código deja de servir.',
            style: context.type.body.copyWith(color: colors.textMuted),
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            confirmLabel: 'Cerrar la sala',
            onCancel: shell.closeDialog,
            // No navega: lo hace el oyente de la sala cuando la sala se va de
            // verdad. Irse antes pintaba la portada con el daemon todavía
            // dentro, y un cierre que falla dejaba la app en una portada que
            // no acepta crear ni entrar.
            onConfirm: () {
              context.read<SessionCubit>().leave();
              shell.closeDialog();
            },
          ),
        ],
      ),
    );
  }
}

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

/// La sala que quedó abierta cuando el daemon murió sin poder despedirse.
///
/// Pregunta y no reabre sola, que es la invariante: nada que llegue de fuera de
/// la app surte efecto sin confirmación dentro, y acá lo de fuera es un archivo
/// del arranque anterior. Ver `docs/05-ui.md` y la decisión 22.
///
/// Mientras esto está en pantalla la máquina YA está en cuarentena: el arranque
/// purga las reglas propias antes de preguntar nada, así que dudar acá no deja
/// ningún puerto abierto.
class ResumeRoomDialog extends StatelessWidget {
  const ResumeRoomDialog({required this.pending, super.key});

  final PendingRoom pending;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final SessionCubit session = context.read<SessionCubit>();

    return AppModal(
      width: 460,
      onDismiss: shell.closeDialog,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text(
            'Tenías una sala abierta',
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          _ResumeRoomBody(pending: pending),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            cancelLabel: 'Cerrarla',
            confirmLabel: 'Reabrir la sala',
            onCancel: () {
              session.discardPendingRoom();
              shell.closeDialog();
            },
            onConfirm: () {
              session.resumePendingRoom();
              shell.closeDialog();
            },
          ),
        ],
      ),
    );
  }
}

/// El cuerpo del aviso, en su propia clase.
///
/// Separado porque su texto depende de datos que pueden faltar —una sala puede
/// no tener nombre— y armarlo dentro del `build` de arriba mezclaría dos cosas
/// que se leen distinto. Es una clase y no un método que devuelva Widget, que
/// es la regla del proyecto: ver la explicación en `shell_page.dart`.
class _ResumeRoomBody extends StatelessWidget {
  const _ResumeRoomBody({required this.pending});

  final PendingRoom pending;

  @override
  Widget build(BuildContext context) {
    final String cual = pending.name.isEmpty
        ? 'La sala con el código ${pending.displayCode}'
        : '"${pending.name}", con el código ${pending.displayCode}';

    return Text(
      '$cual.\n'
      'Se cerró sin avisar, seguramente por un apagón o un reinicio.\n\n'
      'Si la reabres, vuelve con el mismo código y el mismo juego, así que '
      'los enlaces que ya repartiste siguen valiendo. Quien siga esperando '
      'reconecta solo.',
      style: context.type.body.copyWith(color: context.colors.textOnChip),
    );
  }
}
