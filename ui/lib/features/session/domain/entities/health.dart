import 'package:flutter/foundation.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/canary.dart';
import 'package:kanpachi_ui/features/session/domain/entities/returning.dart';

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
    this.net = const NetDiagnostics.unknown(),
    this.seedDown = false,
    this.returning,
  });

  /// Antes de haber preguntado. Ni avisos ni comprobación, que no es lo mismo
  /// que "está todo bien": es que todavía no se sabe.
  const HealthReport.unknown()
    : alerts = const <HealthAlert>[],
      canary = const CanaryCheck.blind(),
      net = const NetDiagnostics.unknown(),
      // Falso porque no se preguntó, no porque conteste. Es el mismo criterio
      // que el resto de este constructor: no saber jamás se pinta como un
      // hallazgo.
      seedDown = false,
      returning = null;

  /// Los avisos vivos, **en el orden que los mandó el daemon**.
  ///
  /// El orden es suyo a propósito: los produce por gravedad, y reordenarlos acá
  /// sería que la pantalla opine sobre algo que ya se decidió donde se puede
  /// medir.
  final List<HealthAlert> alerts;

  /// La última ronda de la Protección Kanpachi.
  final CanaryCheck canary;

  /// Lo que el daemon sabe de la red por la que va el túnel.
  final NetDiagnostics net;

  /// El servidor de encuentro no le contesta al daemon.
  ///
  /// # Por qué acá y no en [alerts]
  ///
  /// Porque `alerts` es un conjunto cerrado de hallazgos de EXPOSICIÓN: cosas
  /// que dejan la máquina alcanzable y que anulan la promesa del producto si
  /// nadie las mira. Esto es disponibilidad, y meterlo ahí diría que la máquina
  /// quedó expuesta cuando lo que pasa es que no se pueden abrir salas nuevas.
  /// Convive con [net] por el mismo motivo: son datos que el daemon produjo
  /// solo y que no son avisos de seguridad.
  ///
  /// # Por qué no en [Room]
  ///
  /// Porque hace falta justo cuando NO hay sala. Es el aviso de la portada, el
  /// que dice que crear y entrar van a fallar antes de que alguien escriba un
  /// código, y en `Room` sería inalcanzable en el único momento en que sirve.
  ///
  /// Distinto de que el daemon no conteste: ahí no se pudo preguntar nada. Acá
  /// el daemon contesta perfectamente y lo que informa es que el registro no le
  /// contesta a él. Se apaga solo en cuanto el registro vuelva.
  final bool seedDown;

  /// La sala a la que esta máquina está volviendo, si está volviendo a alguna.
  ///
  /// Vive acá por el mismo criterio que [seedDown], y con más razón: hace falta
  /// justo cuando NO hay sala, así que en [Room] sería inalcanzable en el único
  /// momento en que sirve. Y llega dentro del mismo `status` que el resto, así
  /// que no cuesta una llamada más.
  final Returning? returning;

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
    net: NetDiagnostics.fromJson(
      (json['net'] as Map<String, Object?>?) ?? const <String, Object?>{},
    ),
    seedDown: json['seed_down'] as bool? ?? false,
    returning: Returning.fromJson(json['returning']),
  );
}

/// Lo que el daemon midió de la red que hay debajo del túnel.
///
/// Vive acá y no en [Room] por el mismo criterio que el resto de este archivo:
/// no lo pidió nadie, lo produjo el daemon solo. Y lo que dice importa aunque la
/// sala esté perfecta: con UDP bloqueado todo el mundo llega por relay, y la
/// partida va lenta sin que ninguna pantalla lo explique.
@immutable
class NetDiagnostics {
  const NetDiagnostics({
    this.natKind,
    this.udpBlocked = false,
    this.mtu,
    this.subnetReason,
  });

  /// Antes de que haya sala no se midió nada, y eso no es "está todo bien".
  const NetDiagnostics.unknown()
    : natKind = null,
      udpBlocked = false,
      mtu = null,
      subnetReason = null;

  /// Qué tipo de NAT hay delante, tal como lo nombra el motor.
  final String? natKind;

  /// UDP no sale. Con esto puesto la sala funciona por relay y va más lenta.
  final bool udpBlocked;

  /// El MTU que quedó puesto en el adaptador virtual.
  final int? mtu;

  /// Por qué se eligió ese rango de direcciones y no otro.
  final String? subnetReason;

  bool get isKnown =>
      natKind != null || mtu != null || udpBlocked || subnetReason != null;

  static NetDiagnostics fromJson(Map<String, Object?> json) {
    final int mtu = (json['mtu'] as num? ?? 0).toInt();
    return NetDiagnostics(
      natKind: json['nat_kind'] as String?,
      udpBlocked: json['udp_blocked'] as bool? ?? false,
      mtu: mtu > 0 ? mtu : null,
      subnetReason: json['subnet_reason'] as String?,
    );
  }
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
