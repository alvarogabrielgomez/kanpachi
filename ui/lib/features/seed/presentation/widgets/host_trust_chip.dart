import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';

/// Si la llave que firmó esta sala es una que esta máquina ya vio, dicho en
/// una etiqueta al lado del servidor.
///
/// # Qué dice, y qué NO dice
///
/// Dice que la llave es la misma con la que jugaste antes, o que no lo es.
/// **No dice de quién es esa llave.** La primera vez no hay con qué comparar,
/// igual que en Signal, y eso se escribe en vez de disimularse: «host nuevo»
/// es un dato, no una advertencia.
///
/// # Por qué una etiqueta y no la caja que había
///
/// Porque el bloque anterior gastaba cuatro líneas y una huella de veinte
/// dígitos para decir «esta llave ya la conocías», encabezado por una cuenta
/// sin sujeto: «La misma llave de siempre, ya en 126 salas». Colgaba debajo
/// del nombre del servidor, así que se leía como si la llave fuera del
/// servidor, y la huella pedía una comparación que nadie iba a hacer con nada.
/// La continuidad es un sí o un no, y cabe al lado del dato que califica.
/// Visto en pantalla el 2026-08-26.
///
/// La huella sigue apareciendo entera, y solo donde sirve: cuando un nombre
/// conocido llega con otra llave, que es la única vez que hay dos huellas que
/// comparar. Ver [HostTrustBlock].
///
/// # Por qué el aviso no bloquea
///
/// Porque la gente reinstala Windows, y reinstalar genera una llave nueva. Un
/// bloqueo dentro de un juego se convierte en un botón que se pulsa sin leer;
/// un aviso con las dos huellas a la vista es algo que se puede comprobar por
/// otro canal. Es el mismo camino que hizo Signal, que empezó bloqueando ante
/// un cambio de llave y se movió a avisar. Decisión 25.
class HostTrustChip extends StatelessWidget {
  const HostTrustChip({required this.invite, super.key});

  final PendingInvite invite;

  /// La frase corta. Nombra al HOST y no a la llave: quien lee esto está
  /// decidiendo con quién juega, y «llave conocida» le hace traducir.
  String get _texto => switch (invite.verdict) {
    HostVerdict.llaveCambiada => 'La llave cambió',
    HostVerdict.conocida || HostVerdict.renombrada => 'Host conocido',
    HostVerdict.nueva => 'Host nuevo',
    HostVerdict.unverified => '',
  };

  /// Lo que el ratón revela, que es donde vive todo lo que la etiqueta ya no
  /// grita. La cuenta de salas entra acá porque a nadie le hace falta para
  /// decidir, y a quien la busca le contesta.
  String get _detalle => switch (invite.verdict) {
    HostVerdict.llaveCambiada =>
      'Ese nombre llegaba antes con otra llave. Compara las dos huellas con '
          'quien te pasó el código.',
    HostVerdict.conocida =>
      invite.knownRooms > 1
          ? 'Ya entraste a una sala firmada con esta llave. Van '
                '${invite.knownRooms}.'
          : 'Ya entraste a una sala firmada con esta llave.',
    HostVerdict.renombrada =>
      'Ya entraste a una sala firmada con esta llave. El nombre cambió.',
    HostVerdict.nueva =>
      'Es la primera vez que entras a una sala firmada con esta llave. Desde '
          'ahora esta ventana la reconoce.',
    HostVerdict.unverified => '',
  };

  @override
  Widget build(BuildContext context) {
    if (invite.verdict == HostVerdict.unverified) {
      return const SizedBox.shrink();
    }
    final colors = context.colors;
    // Verde solo para la continuidad, rojo solo para el aviso, y gris para la
    // primera vez. Una llave nunca vista no es un problema: es lo que pasa en
    // toda primera invitación, y pintarla de ámbar enseñaría a ignorar el
    // ámbar donde sí significa algo.
    final Color color = switch (invite.verdict) {
      HostVerdict.llaveCambiada => colors.danger,
      HostVerdict.conocida || HostVerdict.renombrada => colors.ok,
      HostVerdict.nueva || HostVerdict.unverified => colors.textMuted,
    };
    return Tooltip(
      message: _detalle,
      child: DecoratedBox(
        decoration: BoxDecoration(
          border: Border.all(color: color),
          borderRadius: AppRadius.pill,
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.lg,
            vertical: AppSpacing.xs,
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(_icono, size: 14, color: color),
              const SizedBox(width: AppSpacing.sm),
              Text(_texto, style: context.type.labelSm.copyWith(color: color)),
            ],
          ),
        ),
      ),
    );
  }

  /// La persona para la continuidad, la huella para el aviso: ahí lo que se
  /// pide es comparar dígitos, y el icono manda al sitio donde están.
  IconData get _icono => invite.verdict == HostVerdict.llaveCambiada
      ? Icons.fingerprint_rounded
      : Icons.person_outline_rounded;
}
