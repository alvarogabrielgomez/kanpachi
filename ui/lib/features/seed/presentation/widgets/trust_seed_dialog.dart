import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
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
/// Es el mismo gesto que ya existe DENTRO de la sala para renombrarla: el
/// nombre en texto plano, un lápiz al lado, y el campo aparece al pulsar. Que
/// los dos sitios donde se renombra una sala se pulsen igual es lo que hace que
/// no haya que aprender el segundo. Ver `_NameDisplay` en `room_page.dart`.
class _NombreDeLaSala extends StatefulWidget {
  const _NombreDeLaSala({required this.controller});

  final TextEditingController controller;

  @override
  State<_NombreDeLaSala> createState() => _NombreDeLaSalaState();
}

class _NombreDeLaSalaState extends State<_NombreDeLaSala> {
  bool _editando = false;

  void _cerrar() => setState(() => _editando = false);

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
          child: _editando
              ? AppField(
                  controller: widget.controller,
                  shape: AppFieldShape.inline,
                  maxLength: 24,
                  autofocus: true,
                  onSubmitted: (_) => _cerrar(),
                  onChanged: (String v) =>
                      context.read<SessionCubit>().setRoomNameDraft(v),
                )
              : _NombreEnFirme(
                  nombre: widget.controller.text,
                  onEditar: () => setState(() => _editando = true),
                ),
        ),
      ],
    );
  }
}

/// El nombre en texto plano con su lápiz, y todo el bloque como objetivo.
///
/// El objetivo de clic es el bloque entero y no solo el lápiz, por lo mismo que
/// en la pantalla de sala: el lápiz es chico y quien quiere renombrar pincha el
/// nombre. La marca al pasar por encima es lo que avisa de que se puede.
class _NombreEnFirme extends StatefulWidget {
  const _NombreEnFirme({required this.nombre, required this.onEditar});

  final String nombre;
  final VoidCallback onEditar;

  @override
  State<_NombreEnFirme> createState() => _NombreEnFirmeState();
}

class _NombreEnFirmeState extends State<_NombreEnFirme> {
  bool _encima = false;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return MouseRegion(
      cursor: SystemMouseCursors.text,
      onEnter: (_) => setState(() => _encima = true),
      onExit: (_) => setState(() => _encima = false),
      child: GestureDetector(
        onTap: widget.onEditar,
        child: AnimatedContainer(
          duration: AppMotion.hover,
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.lg,
            vertical: AppSpacing.md,
          ),
          decoration: BoxDecoration(
            color: _encima ? colors.surfaceSunken : null,
            borderRadius: AppRadius.all10,
            border: Border.all(
              color: _encima ? colors.border : Colors.transparent,
              width: AppStroke.hairline,
            ),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Flexible(
                child: Text(
                  widget.nombre,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: context.type.label.copyWith(color: colors.text),
                ),
              ),
              const SizedBox(width: AppSpacing.md),
              Icon(Icons.edit_outlined, size: 15, color: colors.textMuted),
            ],
          ),
        ),
      ),
    );
  }
}
