import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_editable_name.dart';
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

    // Cerrar este diálogo ABANDONA la creación, se cierre por donde se cierre:
    // la cruz, el velo, Escape o «Cancelar». Sin esto, quien viene de elegir
    // servidor y aquí se arrepiente dejaría la intención viva, y la siguiente
    // pantalla que la mire abriría una sala que ya nadie pidió.
    void abandonar() {
      session.dropHostIntent();
      shell.closeDialog();
    }

    return AppModal(
      onDismiss: abandonar,
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
            onCancel: abandonar,
            onConfirm: () async {
              shell.closeDialog();
              // El juego, si lo hubo, sale de la sesión y no de la petición:
              // quien lo eligió fue el diálogo anterior y ahí sigue guardado.
              //
              // **Sin `dropHostIntent` acá.** Confirmar no abandona: la
              // creación que arranca es la que decide, y es ella la que
              // recuerda la intención cuando falla por falta de contraseña.
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
        // El mismo relleno que el aviso de [SeedTrustBlock]. Son las dos cajas
        // grandes del diálogo, una encima de la otra.
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.x5l,
          vertical: AppSpacing.x3l,
        ),
        child: Row(
          children: <Widget>[
            // El glifo va en acento y no en gris: es la única marca de color
            // del bloque y lo que dice que esa línea ES el servidor, no un
            // dato más. `hub` y no `dns` ni `cloud`: un seed no es una nube ni
            // un disco, es el punto donde se encuentran varios.
            Icon(Icons.hub_outlined, size: 17, color: colors.accent),
            const SizedBox(width: AppSpacing.xl),
            // Expanded y no suelto: la caja ocupa el ancho del diálogo, así
            // que el nombre corta con puntos en vez de encoger la caja hasta
            // el largo del texto. Un servidor de nombre corto y uno de nombre
            // largo tienen que dibujar la misma caja.
            Expanded(
              child: Text(
                seed,
                style: context.type.mono.copyWith(color: colors.text),
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
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
///
/// # Por qué se LEE, y solo se escribe si lo pides
///
/// Porque esto no es un formulario: es una confirmación, y el dato que hay que
/// confirmar es el servidor. Un campo de texto permanente al lado de la caja
/// del seed compite con ella y hace parecer que lo que se está decidiendo es
/// cómo llamar a la sala.
///
/// Es el MISMO componente que renombra la sala desde dentro, en su tamaño
/// chico: ver [AppEditableName]. No una copia suya con otras medidas, que es lo
/// que había acá y es lo que hace que dos sitios de la misma app se pulsen
/// distinto.
class _NombreDeLaSala extends StatelessWidget {
  const _NombreDeLaSala({required this.controller});

  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.only(right: AppSpacing.xl),
          child: Text(
            'SALA',
            style: context.type.monoSm.copyWith(
              color: colors.textMuted,
              letterSpacing: 1.6,
            ),
          ),
        ),
        Expanded(
          child: AppEditableName(
            controller: controller,
            size: AppEditableNameSize.sm,
            // Confirmar acá no manda nada a ningún sitio: la sala todavía no
            // existe. Lo que hay que mantener al día es el borrador de la
            // sesión, que es el mismo dato que edita el campo de la portada, y
            // eso ya lo hace `onChanged` en cada tecla.
            onCommit: (String _) {},
            onChanged: (String v) =>
                context.read<SessionCubit>().setRoomNameDraft(v),
          ),
        ),
      ],
    );
  }
}
