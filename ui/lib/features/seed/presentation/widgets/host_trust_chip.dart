import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_glyphs.dart';
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
/// igual que en Signal, y eso se escribe en vez de disimularse: «Host nuevo»
/// es un dato, no una advertencia.
///
/// # Tres capas, y cada una cuesta lo que da
///
/// La etiqueta contesta el sí o el no, que es lo único que hace falta para
/// decidir si entras. El ratón encima cuenta el resto en una frase, la cuenta
/// de salas incluida. Pulsarla abre las huellas, que es lo que se compara
/// carácter a carácter con quien te pasó el código.
///
/// Hasta el 2026-08-26 las tres capas salían a la vez, en una caja de cuatro
/// líneas debajo del servidor: un titular con una cuenta que nadie pidió, «La
/// misma llave de siempre, ya en 126 salas», y veinte dígitos que en ese
/// momento no se contrastaban contra nada. Enseñar una huella sin nada
/// enfrente enseña a pasar de largo por una huella.
///
/// # Por qué el aviso no bloquea
///
/// Porque la gente reinstala Windows, y reinstalar genera una llave nueva. Un
/// bloqueo dentro de un juego se convierte en un botón que se pulsa sin leer;
/// un aviso con las dos huellas a mano es algo que se puede comprobar por otro
/// canal. Es el mismo camino que hizo Signal, que empezó bloqueando ante un
/// cambio de llave y se movió a avisar. Decisión 25.
class HostTrustChip extends StatefulWidget {
  const HostTrustChip({required this.invite, super.key});

  final PendingInvite invite;

  @override
  State<HostTrustChip> createState() => _HostTrustChipState();
}

class _HostTrustChipState extends State<HostTrustChip> {
  /// Anclado por `LayerLink` y no por coordenadas: el panel se pega al borde
  /// de la píldora, que puede estar en un diálogo o en una tarjeta y en cada
  /// sitio cae en otra parte de la pantalla. Es el mismo mecanismo que el menú
  /// de la cuenta en la barra de título.
  final LayerLink _link = LayerLink();
  final OverlayPortalController _portal = OverlayPortalController();

  bool get _alarma => widget.invite.verdict == HostVerdict.llaveCambiada;

  /// La frase corta. Nombra al HOST y no a la llave: quien lee esto está
  /// decidiendo con quién juega, y «llave conocida» le hace traducir.
  String get _texto => switch (widget.invite.verdict) {
    HostVerdict.llaveCambiada => 'La llave cambió',
    HostVerdict.conocida || HostVerdict.renombrada => 'Host conocido',
    HostVerdict.nueva => 'Host nuevo',
    HostVerdict.unverified => '',
  };

  /// Lo que el ratón revela. La cuenta de salas vive acá porque a nadie le
  /// hace falta para decidir, y a quien la busca le contesta.
  String get _detalle => switch (widget.invite.verdict) {
    HostVerdict.llaveCambiada =>
      'Ese nombre llegaba antes con otra llave. Pulsa para comparar las dos '
          'huellas con quien te pasó el código.',
    HostVerdict.conocida =>
      widget.invite.knownRooms > 1
          ? 'Ya entraste a una sala firmada con esta llave. Van '
                '${widget.invite.knownRooms}. Pulsa para ver la huella.'
          : 'Ya entraste a una sala firmada con esta llave. Pulsa para ver la '
                'huella.',
    HostVerdict.renombrada =>
      'Ya entraste a una sala firmada con esta llave. El nombre cambió. Pulsa '
          'para ver la huella.',
    HostVerdict.nueva =>
      'Es la primera vez que entras a una sala firmada con esta llave. Desde '
          'ahora esta ventana la reconoce. Pulsa para ver la huella.',
    HostVerdict.unverified => '',
  };

  /// La persona para la continuidad, la huella para el aviso: ahí lo que se
  /// pide es comparar dígitos, y el icono dice dónde están.
  IconData get _icono =>
      _alarma ? Icons.fingerprint_rounded : Icons.person_outline_rounded;

  @override
  Widget build(BuildContext context) {
    if (widget.invite.verdict == HostVerdict.unverified) {
      return const SizedBox.shrink();
    }
    final colors = context.colors;
    // Verde solo para la continuidad, rojo solo para el aviso, y gris para la
    // primera vez. Una llave nunca vista no es un problema: es lo que pasa en
    // toda primera invitación, y pintarla de ámbar enseñaría a ignorar el
    // ámbar donde sí significa algo.
    final Color color = switch (widget.invite.verdict) {
      HostVerdict.llaveCambiada => colors.danger,
      HostVerdict.conocida || HostVerdict.renombrada => colors.ok,
      HostVerdict.nueva || HostVerdict.unverified => colors.textMuted,
    };
    return OverlayPortal(
      controller: _portal,
      overlayChildBuilder: (BuildContext context) => Stack(
        children: <Widget>[
          // Pulsar fuera cierra. Va debajo del panel y encima de todo lo
          // demás, que es lo que se espera de algo que se abrió pulsando.
          Positioned.fill(
            child: GestureDetector(
              behavior: HitTestBehavior.opaque,
              onTap: _portal.hide,
            ),
          ),
          CompositedTransformFollower(
            link: _link,
            targetAnchor: Alignment.bottomRight,
            followerAnchor: Alignment.topRight,
            offset: const Offset(0, AppSpacing.sm),
            child: _PanelDeHuellas(invite: widget.invite),
          ),
        ],
      ),
      child: CompositedTransformTarget(
        link: _link,
        child: Tooltip(
          message: _detalle,
          child: MouseRegion(
            cursor: SystemMouseCursors.click,
            child: GestureDetector(
              // `opaque` y no el diferido por omisión: sin esto solo responden
              // los píxeles pintados, o sea el icono y las letras, y los
              // huecos entre ellos parecen el botón sin serlo.
              behavior: HitTestBehavior.opaque,
              onTap: _portal.toggle,
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
                      Text(
                        _texto,
                        style: context.type.labelSm.copyWith(color: color),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Lo que se compara carácter a carácter, y solo cuando alguien lo pide.
///
/// Con la llave cambiada trae DOS y llevan etiqueta, porque lo que se pide es
/// compararlas y sin nombre no se sabe cuál es cuál. Con una sola no hay nada
/// que distinguir, así que en su sitio va el icono de la huella: la palabra
/// HUELLA delante de una huella no agrega nada.
class _PanelDeHuellas extends StatelessWidget {
  const _PanelDeHuellas({required this.invite});

  final PendingInvite invite;

  /// El ancho lo fija el panel y no su contenido: son cinco grupos de cuatro
  /// dígitos en monoespaciada, siempre los mismos, y un panel que cambia de
  /// tamaño según el veredicto se lee como dos componentes distintos.
  static const double _ancho = 320;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final bool alarma = invite.verdict == HostVerdict.llaveCambiada;
    return Material(
      color: Colors.transparent,
      child: Container(
        width: _ancho,
        decoration: BoxDecoration(
          color: colors.surface,
          border: Border.all(color: alarma ? colors.danger : colors.border),
          borderRadius: AppRadius.allLg,
          boxShadow: <BoxShadow>[
            BoxShadow(
              color: colors.shadowMenu,
              blurRadius: 24,
              offset: const Offset(0, 8),
            ),
          ],
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.x3l,
            vertical: AppSpacing.xl,
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              if (alarma) ...<Widget>[
                const _Aviso(),
                const SizedBox(height: AppSpacing.lg),
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
                _Huella(valor: invite.fingerprint, resaltada: true),
            ],
          ),
        ),
      ),
    );
  }
}

/// Qué hacer con las dos huellas, dicho donde están las dos huellas.
class _Aviso extends StatelessWidget {
  const _Aviso();

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
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
        Expanded(
          child: Text(
            'Puede ser un Windows reinstalado, y puede no ser la misma '
            'persona. Compara las dos con quien te pasó el código.',
            style: context.type.bodySm.copyWith(
              color: colors.text,
              height: 1.55,
            ),
          ),
        ),
      ],
    );
  }
}

/// Una huella, en monoespaciada.
///
/// Sin etiqueta lleva el icono de la huella en su sitio, con el mismo ancho
/// que ocupaba la palabra, así las dos formas del panel alinean su columna de
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
