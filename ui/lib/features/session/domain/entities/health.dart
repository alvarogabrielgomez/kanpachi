import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/canary.dart';

/// Lo que el daemon vigila SOLO, sin que nadie se lo pida.
///
/// # Por qué no es [ExposureReport] ni [ProbeReport]
///
/// Los otros dos son mediciones que el USUARIO pide, con su botón y su pantalla.
/// Esto lo produjo el daemon por su cuenta mientras nadie miraba, y por eso la
/// forma de leerlo es al revés: llega solo, y la pantalla lo pinta sin haber
/// preguntado nada.
///
/// # Por qué vive fuera de [Room]
///
/// Porque la mitad de esto existe SIN sala. Que el Firewall de Windows esté
/// apagado o que el router publique un puerto es cierto en la portada, antes de
/// que haya nada que hospedar, y es justo cuando conviene enterarse. Colgarlo de
/// la sala lo escondería hasta que el usuario abriera una.
@immutable
class HealthReport {
  const HealthReport({
    this.alerts = const <HealthAlert>[],
    this.canary = const CanaryCheck.blind(),
  });

  /// Antes de haber preguntado. Ni avisos ni comprobación, que no es lo mismo
  /// que "está todo bien": es que todavía no se sabe.
  const HealthReport.unknown()
      : alerts = const <HealthAlert>[],
        canary = const CanaryCheck.blind();

  /// Los avisos vivos, **en el orden que los mandó el daemon**.
  ///
  /// El orden es suyo a propósito: los produce por gravedad, y reordenarlos acá
  /// sería que la pantalla opine sobre algo que ya se decidió donde se puede
  /// medir.
  final List<HealthAlert> alerts;

  /// La última ronda de la Protección Kanpachi.
  final CanaryCheck canary;

  bool get hasAlerts => alerts.isNotEmpty;

  /// Las clases de aviso que esta versión de la UI sabe nombrar.
  ///
  /// Deja fuera las desconocidas, y eso NO pierde el aviso: el desconocido se
  /// sigue pintando por [HealthAlert.wire] con el mensaje de reserva. Esta lista
  /// existe para preguntar "¿está tal alerta?", y de una que no se conoce no se
  /// puede preguntar nada.
  List<AlertKind> get kinds => <AlertKind>[
        for (final HealthAlert a in alerts)
          if (a.kind != null) a.kind!,
      ];

  static HealthReport fromJson(Map<String, Object?> json) => HealthReport(
        alerts: HealthAlert.listFrom(json['alerts']),
        canary: CanaryCheck.fromJson(
          (json['canary'] as Map<String, Object?>?) ?? const <String, Object?>{},
        ),
      );
}

/// Un aviso del módulo de exposición, tal como llegó.
///
/// Lleva la cadena del cable ADEMÁS de la clase, y esa redundancia es la que
/// impide perder un aviso. El daemon y la UI se actualizan por separado, así que
/// una clave que este enum no tiene va a existir tarde o temprano: con la cadena
/// guardada, el catálogo puede darle el mensaje de reserva y el usuario se
/// entera igual. Con solo el enum, el aviso desaparecería en silencio, que es la
/// peor forma de fallar de algo que existe para avisar.
@immutable
class HealthAlert {
  const HealthAlert({required this.wire, this.kind, this.detail});

  /// La cadena exacta del campo `kind` de `AlertView`.
  final String wire;

  /// La clase, o null si esta versión de la UI no la conoce.
  final AlertKind? kind;

  /// El dato que puso el daemon. El copy es del producto y el dato es suyo.
  final String? detail;

  /// Saca los avisos de la lista `alerts` de `RoomView`.
  ///
  /// Nada de lo que llegue puede romper la pantalla: una lista ausente o con
  /// basura dentro da la lista vacía, que además es el caso NORMAL.
  static List<HealthAlert> listFrom(Object? crudo) {
    if (crudo is! List<Object?>) return const <HealthAlert>[];

    final List<HealthAlert> out = <HealthAlert>[];
    for (final Object? item in crudo) {
      if (item is! Map<String, Object?>) continue;
      final HealthAlert? a = fromJson(item);
      if (a != null) out.add(a);
    }
    return out;
  }

  /// Null cuando no hay clave. Un elemento sin `kind` no es una alerta a
  /// medias: no es una alerta. Sin clave no hay texto que elegir, ni siquiera
  /// el de reserva.
  static HealthAlert? fromJson(Map<String, Object?> json) {
    final Object? kind = json['kind'];
    if (kind is! String || kind.isEmpty) return null;

    return HealthAlert(
      wire: kind,
      kind: AlertKind.fromWire(kind),
      detail: json['detail'] as String?,
    );
  }
}
