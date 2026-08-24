import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_dialog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/displacement.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// Entrar a una sala cuando entrar cuesta algo.
///
/// # Los tres casos son tres pérdidas distintas
///
/// Y por eso no comparten texto. Cerrar la sala propia **la termina**: se borra
/// su fichero, se retira del registro, su código deja de resolver y no queda
/// nada que reabrir. Salir de la sala de otro no destruye nada, y lo que sí
/// cambia es que esa sala deja de ser una a la que se vuelva sola. Dejar de
/// volver no cuesta nada que exista todavía: se apaga un reloj.
///
/// La primera es la acción más destructiva de la aplicación, y hasta acá se
/// anunciaba como si fuera salir.
///
/// # Vive en el shell y ya no en la portada
///
/// Porque dejó de ser de la portada. Lo abren también la pantalla del enlace
/// `kanpachi://`, que se enseña con la sala abierta detrás, y el aviso de la
/// sala propia guardada.
///
/// # Va ANTES del diálogo de confianza
///
/// Se pregunta primero por lo que se pierde y después por la máquina a la que se
/// va a hablar. Quien vaya a decir que no, que lo diga antes de haber leído un
/// párrafo sobre un registro que ya no va a usar.
class ConfirmDisplaceDialog extends StatelessWidget {
  const ConfirmDisplaceDialog({
    required this.displaces,
    required this.onConfirm,
    super.key,
  });

  /// Lo que se deja atrás, tal como lo contestó el daemon.
  final Displacement displaces;

  /// Lo que sigue si se confirma. Quien abrió esto es quien sabe adónde iba, así
  /// que el camino no se decide acá.
  final VoidCallback onConfirm;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final _Copy copy = _Copy.para(displaces);

    return AppModal(
      width: 420,
      onDismiss: shell.closeDialog,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text(
            copy.title,
            style: context.type.titleXs.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.xl),
          Text(
            copy.body,
            style: context.type.body.copyWith(color: colors.textOnChip),
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppModalActions(
            cancelLabel: copy.cancel,
            confirmLabel: copy.confirm,
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

/// Las cuatro frases de un caso.
///
/// Una clase privada y un `switch` en vez de cuatro `switch` sueltos dentro del
/// `build`: así una clase nueva del daemon obliga a escribir sus cuatro frases
/// juntas, en vez de dejar tres puestas y una olvidada.
class _Copy {
  const _Copy({
    required this.title,
    required this.body,
    required this.cancel,
    required this.confirm,
  });

  /// El texto de cada caso. Lo desconocido cae en el genérico y **se pregunta
  /// igual**: un daemon más nuevo no puede conseguir que se entre a una sala sin
  /// preguntar, solo que se pregunte con menos detalle.
  factory _Copy.para(Displacement d) {
    final String nombre = d.label;
    return switch (d.kind) {
      DisplaceKind.closeRoom => _Copy(
        title: 'Cerrar tu sala $nombre',
        body:
            'Entrar a otra sala cierra la tuya, y cerrarla es definitivo: su '
            'código deja de funcionar, se borra del servidor y no queda nada '
            'que reabrir.\n\n'
            '${_quienesCaen(d.members)} Para volver a jugar juntos habría que '
            'abrir una sala nueva y repartir su código otra vez.',
        cancel: 'Dejarla abierta',
        confirm: 'Cerrarla y entrar',
      ),
      DisplaceKind.leaveRoom => _Copy(
        title: 'Salir de $nombre',
        body:
            'Estás dentro de esa sala, y entrar a otra te saca de ella.\n\n'
            'Su código queda guardado, así que puedes volver a entrar cuando '
            'quieras. Lo que se apaga es que se vuelva sola.',
        cancel: 'Quedarme',
        confirm: 'Salir y entrar',
      ),
      DisplaceKind.stopReturning => _Copy(
        title: 'Dejar de volver a $nombre',
        body:
            'Estás esperando a volver a esa sala, y esto lo cancela.\n\n'
            'Su código queda guardado igual, así que puedes volver a entrar '
            'cuando quieras. Lo que se apaga es que se intente solo.',
        cancel: 'Seguir esperando',
        confirm: 'Dejar de esperar',
      ),
      // Incluye `nothing`, que no debería llegar acá: quien abre este diálogo ya
      // comprobó que hay algo. Si llegara, se pregunta.
      _ => _Copy(
        title: 'Dejar atrás $nombre',
        body:
            'Entrar a otra sala deja atrás esa. Esta versión de la app no sabe '
            'explicar exactamente qué se pierde, así que conviene mirarlo en '
            '`kanpachi status` antes de seguir.',
        cancel: 'Cancelar',
        confirm: 'Dejarla y entrar',
      ),
    };
  }

  final String title;
  final String body;
  final String cancel;
  final String confirm;

  /// Quién se cae al cerrar. Cero personas dentro es el caso normal de una sala
  /// que se abrió y nadie usó, y decir «0 personas» ahí sería inventar un drama.
  static String _quienesCaen(int members) => switch (members) {
    <= 0 => 'No hay nadie dentro ahora mismo.',
    1 =>
      'La persona que está dentro se cae y los puertos del juego se cierran.',
    _ =>
      'Las $members personas que están dentro se caen y los puertos del juego '
          'se cierran.',
  };
}
