import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_glyphs.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';

/// Las dos huellas, cuando un nombre conocido llega con otra llave.
///
/// # Por qué solo ese caso
///
/// Porque es el único que pide comparar algo. Hasta el 2026-08-26 esta caja
/// salía también con la llave de siempre y con una llave nueva, y entonces
/// enseñaba una huella suelta: veinte dígitos que no se contrastan contra
/// nada, encabezados por «La misma llave de siempre, ya en 126 salas». Eso
/// enseña a pasar de largo por una huella, que es justo lo que no puede pasar
/// acá. Los otros tres veredictos caben en la etiqueta que va pegada al
/// servidor, ver [HostTrustChip].
///
/// # Qué dice, y qué NO dice
///
/// Dice que la llave que firmó esta sala no es la misma con la que jugaste
/// antes. **No dice de quién es ninguna de las dos.** Comparar los dígitos con
/// quien te pasó el código es lo único que las ata a una persona.
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
            const SizedBox(height: AppSpacing.lg),
            // **Con alarma hay dos huellas y llevan etiqueta**, porque lo que
            // se pide es compararlas y sin nombre no se sabe cuál es cuál. Con
            // una sola no hay nada que distinguir, así que en su sitio va el
            // icono de la huella: la palabra HUELLA delante de una huella no
            // agrega nada, y su columna de ancho fijo era lo que dejaba la
            // fila desalineada del texto de arriba. Visto en pantalla el
            // 2026-08-18.
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
              _Huella(valor: invite.fingerprint, resaltada: false),
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

  /// **Sin nombres, y no por falta de sitio.** Este bloque habla de una LLAVE,
  /// y de quién es esa llave no dice nada: eso es lo que lo separa de una
  /// cuenta verificada, y lo dice el comentario de [HostTrustBlock] desde el
  /// primer día. El nombre además casi nunca llega, así que el texto caía en
  /// un «Quien hospeda» que se leía como el nombre de alguien. Visto en
  /// pantalla el 2026-08-18.
  String get _texto => switch (invite.verdict) {
    HostVerdict.llaveCambiada =>
      'Ojo: esta sala viene firmada con otra llave. Puede ser un Windows '
          'reinstalado, y puede no ser la misma persona. Compara las dos '
          'huellas con quien te pasó el código.',
    // Los otros cuatro no llegan acá: la caja sale solo con la llave
    // cambiada, y lo que dicen lo dice [HostTrustChip]. Se contestan igual
    // porque un switch sin rama es un fallo de compilación, y porque quien
    // vuelva a montar esta caja en otro sitio se merece una frase y no un
    // hueco.
    HostVerdict.conocida =>
      invite.knownRooms > 1
          ? 'La misma llave de siempre, ya en ${invite.knownRooms} salas.'
          : 'La misma llave con la que ya jugaste.',
    HostVerdict.renombrada =>
      'La misma llave con la que ya jugaste. El nombre cambió.',
    HostVerdict.nueva =>
      'Primera vez con esta llave. Desde ahora esta ventana la reconoce.',
    HostVerdict.unverified => '',
  };

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool alarma = invite.verdict == HostVerdict.llaveCambiada;
    // **El icono solo con alarma.** El de la huella se mudó a la huella, y
    // dos huellas en la misma caja eran una de más. Sin icono el texto ocupa
    // la caja entera, que es lo que hace que una frase de una línea se lea
    // como una frase y no como un aviso.
    final Widget texto = Text(
      _texto,
      style: context.type.bodySm.copyWith(
        color: alarma ? colors.text : colors.textMuted,
        height: 1.55,
      ),
    );
    if (!alarma) return texto;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.only(top: 2),
          child: IconTheme(
            data: IconThemeData(color: colors.danger),
            child: const WarnGlyph(),
          ),
        ),
        const SizedBox(width: AppSpacing.md),
        Expanded(child: texto),
      ],
    );
  }
}

/// Una huella, en monoespaciada, que es lo que se compara carácter a carácter.
///
/// Sin etiqueta lleva el icono de la huella en su sitio, con el mismo ancho
/// que ocupaba la palabra, así las dos formas de la caja alinean su columna de
/// dígitos en la misma vertical.
class _Huella extends StatelessWidget {
  const _Huella({required this.valor, required this.resaltada, this.etiqueta});

  final String? etiqueta;
  final String valor;
  final bool resaltada;

  static const double _anchoEtiqueta = 54;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    if (valor.isEmpty) return const SizedBox.shrink();
    final String? e = etiqueta;
    return Row(
      children: <Widget>[
        SizedBox(
          width: _anchoEtiqueta,
          child: e == null
              ? Icon(
                  Icons.fingerprint_rounded,
                  size: 19,
                  color: colors.textMuted,
                )
              : Text(
                  e,
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
