import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_glyphs.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';

/// Quién hospeda, según lo que esta máquina recuerda.
///
/// # Qué dice, y qué NO dice
///
/// Dice que la llave que firmó esta sala es la misma con la que jugaste antes,
/// o que no lo es. **No dice de quién es esa llave.** La primera vez no hay con
/// qué comparar, igual que en Signal, y eso se escribe en vez de disimularse:
/// «es la primera vez» es una frase, no una advertencia.
///
/// # Por qué el aviso no bloquea
///
/// Porque la gente reinstala Windows, y reinstalar genera una llave nueva. Un
/// bloqueo dentro de un juego se convierte en un botón que se pulsa sin leer;
/// un aviso con las dos huellas a la vista es algo que se puede comprobar por
/// otro canal. Es el mismo camino que hizo Signal, que empezó bloqueando ante
/// un cambio de llave y se movió a avisar. Decisión 25.
class HostTrustBlock extends StatelessWidget {
  const HostTrustBlock({required this.invite, super.key});

  final PendingInvite invite;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool alarma = invite.verdict == HostVerdict.llaveCambiada;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: colors.surfaceSunken,
        border: Border.all(color: alarma ? colors.danger : colors.border),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Padding(
        // El mismo relleno que las otras dos cajas grandes del diálogo. Ver
        // `SeedTrustBlock`.
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.x5l,
          vertical: AppSpacing.x3l,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            _Titular(invite: invite),
            const SizedBox(height: AppSpacing.md),
            if (alarma) ...<Widget>[
              _Huella(
                etiqueta: 'ANTES',
                valor: invite.knownFingerprint,
                resaltada: false,
              ),
              const SizedBox(height: AppSpacing.sm),
              _Huella(
                etiqueta: 'AHORA',
                valor: invite.fingerprint,
                resaltada: true,
              ),
            ] else
              _Huella(
                etiqueta: 'HUELLA',
                valor: invite.fingerprint,
                resaltada: false,
              ),
          ],
        ),
      ),
    );
  }
}

/// La frase de arriba, que es lo único que mucha gente va a leer.
class _Titular extends StatelessWidget {
  const _Titular({required this.invite});

  final PendingInvite invite;

  /// El nombre con el que se identifica, o algo neutro cuando no vino tarjeta.
  /// Jamás se afirma que sea quien dice: eso es la decisión 21.
  String get _nick =>
      invite.hostNick.isEmpty ? 'Quien hospeda' : invite.hostNick;

  String get _texto => switch (invite.verdict) {
    HostVerdict.llaveCambiada =>
      'Ojo: $_nick te invitó antes con otra llave. Puede que haya reinstalado '
          'Windows, y puede que no sea la misma persona. Compara las dos '
          'huellas con quien te pasó el código antes de entrar.',
    HostVerdict.conocida => invite.knownRooms > 1
        ? 'Ya jugaste con $_nick, en ${invite.knownRooms} salas. Es la misma '
              'llave de siempre.'
        : 'Ya jugaste con $_nick. Es la misma llave de la otra vez.',
    HostVerdict.renombrada =>
      'Es la llave de ${invite.knownNick}, con la que ya jugaste. Ahora se '
          'identifica como $_nick.',
    HostVerdict.nueva =>
      'Es la primera vez que entras a una sala de $_nick. No hay con qué '
          'comparar todavía: a partir de ahora esta ventana lo reconoce.',
    HostVerdict.unverified => '',
  };

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool alarma = invite.verdict == HostVerdict.llaveCambiada;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: IconTheme(
            data: IconThemeData(color: alarma ? colors.danger : colors.accent),
            child: alarma
                ? const WarnGlyph()
                : const Icon(Icons.fingerprint_rounded, size: 17),
          ),
        ),
        const SizedBox(width: AppSpacing.md),
        Expanded(
          child: Text(
            _texto,
            style: context.type.bodySm.copyWith(
              color: alarma ? colors.text : colors.textMuted,
              height: 1.55,
            ),
          ),
        ),
      ],
    );
  }
}

/// Una huella, en monoespaciada, que es lo que se compara carácter a carácter.
class _Huella extends StatelessWidget {
  const _Huella({
    required this.etiqueta,
    required this.valor,
    required this.resaltada,
  });

  final String etiqueta;
  final String valor;
  final bool resaltada;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    if (valor.isEmpty) return const SizedBox.shrink();
    return Row(
      children: <Widget>[
        SizedBox(
          width: 54,
          child: Text(
            etiqueta,
            style: context.type.monoSm.copyWith(
              color: colors.textMuted,
              letterSpacing: 1.2,
            ),
          ),
        ),
        Expanded(
          child: Text(
            valor,
            style: context.type.mono.copyWith(
              color: resaltada ? colors.text : colors.textMuted,
            ),
          ),
        ),
      ],
    );
  }
}
