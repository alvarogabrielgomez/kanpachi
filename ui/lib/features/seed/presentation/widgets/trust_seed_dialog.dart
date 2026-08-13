import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/seed/presentation/widgets/seed_trust_block.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// Confiar en un registro antes de hablarle.
///
/// # Por qué aparece SIEMPRE, y en los dos momentos
///
/// Porque el registro dejó de venir de fábrica y ahora lo levanta cualquiera.
/// Es el punto de encuentro de la sala: ve la IP pública de quien pregunta y
/// por él pasa todo el mundo de esa sala. Que la máquina sea de un tercero es
/// la decisión, y es de la persona.
///
/// Es UN diálogo para los dos momentos porque es la misma decisión. Lo que
/// cambia es de dónde salió el nombre —al abrir, de lo que esta máquina tiene
/// configurado; al entrar, del código que le pegaron— y que al abrir se puede
/// además elegir cómo se va a llamar la sala.
///
/// # Por qué no hay «no volver a preguntar»
///
/// Porque el nombre puede ser distinto cada vez. Recordar un sí de ayer sobre
/// un servidor no dice nada sobre el que trae el código de hoy, y el momento en
/// que un recordatorio así valdría algo es justo el momento en que dejaría
/// pasar a un desconocido sin que nadie lo lea.
class TrustSeedDialog extends StatefulWidget {
  const TrustSeedDialog({required this.request, super.key});

  final TrustRequest request;

  @override
  State<TrustSeedDialog> createState() => _TrustSeedDialogState();
}

class _TrustSeedDialogState extends State<TrustSeedDialog> {
  late final TextEditingController _nombre;

  @override
  void initState() {
    super.initState();
    // El borrador de la sesión, que es el MISMO que edita el campo de la
    // portada. Vacío significa que nadie escribió nada todavía, y entonces se
    // arranca con la sugerencia que la portada estaba enseñando: así lo que se
    // lee acá es lo que se va a abrir, sin tener que adivinarlo.
    final String draft = context.read<SessionCubit>().state.roomNameDraft;
    _nombre = TextEditingController(
      text: draft.isEmpty ? widget.request.suggestedName : draft,
    );
  }

  @override
  void dispose() {
    _nombre.dispose();
    super.dispose();
  }

  String get _nombreFinal {
    final String v = _nombre.text.trim();
    return v.isEmpty ? widget.request.suggestedName : v;
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final SessionCubit session = context.read<SessionCubit>();
    final bool entrando = widget.request.joining;

    return AppModal(
      onDismiss: shell.closeDialog,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          AppKicker(entrando ? 'Entrar a una sala' : 'Crear una sala'),
          const SizedBox(height: AppSpacing.md),
          Text(
            'Confirma el seed',
            style: context.type.titleSm.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          _FilaDelSeed(seed: widget.request.seed),
          if (!entrando) ...<Widget>[
            const SizedBox(height: AppSpacing.lg),
            _NombreDeLaSala(controller: _nombre),
          ],
          const SizedBox(height: AppSpacing.lg),
          SeedTrustBlock(joining: entrando),
          const SizedBox(height: AppSpacing.x5l),
          AppModalActions(
            stretch: true,
            confirmLabel: entrando ? 'Confiar y entrar' : 'Confiar y crear',
            onCancel: shell.closeDialog,
            onConfirm: () async {
              shell.closeDialog();
              // El juego, si lo hubo, sale de la sesión y no de la petición:
              // quien lo eligió fue el diálogo anterior y ahí sigue guardado.
              final bool ok = entrando
                  ? await session.joinRoom(widget.request.code)
                  : await session.createRoom(
                      name: _nombreFinal,
                      game: session.state.pendingGame,
                    );
              if (ok) shell.go(AppScreen.room);
            },
          ),
        ],
      ),
    );
  }
}

/// La dirección del registro, que es el dato entero del diálogo.
///
/// En monoespaciada y sobre fondo hundido: es lo que alguien COMPARA contra lo
/// que le mandó su amigo, carácter a carácter, y una proporcional hace que `rn`
/// y `m` se parezcan justo donde no pueden parecerse.
class _FilaDelSeed extends StatelessWidget {
  const _FilaDelSeed({required this.seed});

  final String seed;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        border: Border.all(color: colors.border),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.xl,
          vertical: AppSpacing.lg,
        ),
        child: Text(
          seed,
          style: context.type.mono.copyWith(color: colors.text),
          overflow: TextOverflow.ellipsis,
        ),
      ),
    );
  }
}

/// Cómo se va a llamar la sala, editable acá mismo.
///
/// # Por qué se puede escribir en dos sitios
///
/// Porque el nombre se elige en la portada y se confirma acá, y obligar a
/// volver atrás para cambiar una letra sería mandar a cerrar el diálogo que
/// pregunta si confías. Los dos campos son el MISMO dato: lo que se escriba en
/// uno se ve en el otro. Ver `SessionState.roomNameDraft`.
class _NombreDeLaSala extends StatelessWidget {
  const _NombreDeLaSala({required this.controller});

  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.only(right: AppSpacing.md),
          child: Text(
            'SALA',
            style: context.type.monoSm.copyWith(
              color: colors.textMuted,
              letterSpacing: 1.6,
            ),
          ),
        ),
        Expanded(
          child: AppField(
            controller: controller,
            maxLength: 24,
            onChanged: (String v) =>
                context.read<SessionCubit>().setRoomNameDraft(v),
          ),
        ),
      ],
    );
  }
}
