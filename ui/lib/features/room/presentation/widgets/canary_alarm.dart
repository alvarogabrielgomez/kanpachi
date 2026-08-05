import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/canary.dart';

/// La alarma de la Protección Kanpachi, con el botón que la repone.
///
/// # Se enciende por la ALERTA, no por el veredicto
///
/// Y esa diferencia es la que hace que el diseño funcione. El daemon repara la
/// primera fuga SOLO y sin avisar, y solo levanta la alerta si la reposición no
/// aguantó. O sea que:
///
///   - una fuga ya reparada tiene veredicto `leaking` y NO tiene alerta, y no
///     debe pintar nada: enterar al usuario de un problema que Kanpachi ya
///     arregló es enseñarle a ignorar los avisos;
///   - una ronda que nadie contestó tiene veredicto `unconfirmed` con la alerta
///     todavía puesta, y no debe borrar el banner: la alarma sigue siendo cierta.
///
/// Yendo por el veredicto se rompen las dos: pintaría la primera y borraría la
/// segunda.
///
/// El [check] se usa solo para el DETALLE, que es lo que la cadena `detail` de
/// una alerta no sabe expresar con formato: el puerto y a cuántos se preguntó.
class CanaryAlarm extends StatelessWidget {
  const CanaryAlarm({
    required this.alerts,
    required this.check,
    required this.onReapply,
    this.busy = false,
    super.key,
  });

  /// Las alertas vivas del daemon. La alarma se pinta si está la del canario.
  final List<AlertKind> alerts;

  /// La última comprobación, para el detalle.
  final CanaryCheck check;

  final Future<void> Function() onReapply;

  /// Mientras se repone. Deshabilita el botón, que es lo que impide el doble
  /// clic sobre algo que toca el firewall.
  final bool busy;

  @override
  Widget build(BuildContext context) {
    if (!alerts.contains(AlertKind.canaryLeaking)) {
      return const SizedBox.shrink();
    }

    return AppMessageNotice(
      message: AppMessages.alert(AlertKind.canaryLeaking, detail: _detalle),
      actions: <Widget>[
        ReapplyProtectionButton(
          onPressed: busy ? null : () => onReapply(),
          busy: busy,
        ),
      ],
    );
  }

  /// El dato concreto, que es lo que hace creíble la frase. Vacío si no hay
  /// comprobación, y entonces el aviso se queda con su texto de catálogo.
  String get _detalle {
    if (!check.measured || check.port == 0) return '';
    return 'Puerto ${check.port}, comprobado a las ${check.measuredLabel} '
        'con ${check.asked.length} en la sala.';
  }
}

/// El botón que repone la protección.
///
/// Es IDEMPOTENTE: el daemon calcula la diferencia contra las reglas vivas del
/// firewall, así que pulsarlo con nada roto no toca nada. Por eso se puede
/// ofrecer sin preguntar nada y sin diálogo de confirmación.
///
/// Se deshabilita mientras trabaja, y eso no es cosmético: sin ello, el doble
/// clic manda dos escrituras del firewall a la vez.
class ReapplyProtectionButton extends StatelessWidget {
  const ReapplyProtectionButton({
    required this.onPressed,
    this.busy = false,
    super.key,
  });

  final VoidCallback? onPressed;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    return FilledButton(
      onPressed: onPressed,
      child: Text(busy ? 'Reponiendo…' : 'Volver a aplicar la protección'),
    );
  }
}
