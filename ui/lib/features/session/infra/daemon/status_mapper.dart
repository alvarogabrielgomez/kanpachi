import 'package:kanpachi_ui/core/messages/app_message.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';

/// Lo que el daemon dice del estado, ya traducido a lo que la UI pinta.
///
/// Es el punto donde se juntan las dos mitades: el daemon manda claves y el
/// catálogo pone el texto. Nadie más en la app vuelve a mirar una cadena del
/// cable.
class DaemonStatus {
  const DaemonStatus({required this.conn, required this.alerts, this.lastExit});

  /// En qué anda la conexión. Ausente o desconocido cuenta como [ConnState.idle],
  /// que es el estado por defecto y no un error.
  final ConnState conn;

  /// Los avisos del módulo de exposición, ya resueltos a texto y **en el orden
  /// que los mandó el daemon**.
  ///
  /// El orden es suyo a propósito: el daemon los produce por gravedad, y
  /// reordenarlos acá sería que la pantalla opine sobre algo que ya se decidió
  /// donde se puede medir.
  final List<AppMessage> alerts;

  /// Por qué terminó la sesión anterior, si terminó sola.
  final AppMessage? lastExit;

  bool get hasAlerts => alerts.isNotEmpty;
}

/// Traduce la respuesta de `status` a [DaemonStatus].
///
/// # La regla que sigue, y por qué
///
/// **Nada de lo que llega puede romper la pantalla.** El daemon y la UI se
/// actualizan por separado, así que una respuesta con un campo de más, uno de
/// menos, o una clave que esta versión no conoce va a existir tarde o temprano.
/// Cada caso tiene una respuesta decidida acá:
///
///  - Clave de alerta desconocida: sale el mensaje de reserva, con el detalle
///    del daemon pegado. Se pierde el copy bueno y no se pierde el aviso, que
///    es lo que importa.
///  - Estado de conexión desconocido: cuenta como `idle`. Es el único valor
///    seguro, porque no promete que haya sala.
///  - `alerts` ausente o con basura dentro: lista vacía, sin excepción. Que el
///    módulo de exposición no reporte nada es el caso NORMAL.
///
/// Lo que sí se pierde en silencio se pierde a propósito, y está escrito arriba
/// de cada caso. Lo que no se puede es tirar una excepción y dejar la ventana
/// en blanco porque el daemon mandó un campo raro.
abstract final class StatusMapper {
  static DaemonStatus fromJson(Map<String, Object?> json) {
    return DaemonStatus(
      conn: ConnState.fromWire(json['conn'] as String?) ?? ConnState.idle,
      alerts: alertsFrom(json['alerts']),
      lastExit: _exitFrom(json['last_exit']),
    );
  }

  /// Saca los avisos de la lista `alerts` de `RoomView` y les pone el texto.
  ///
  /// Cada elemento es `{kind, detail}`. El `kind` elige el texto y el `detail`
  /// lo acompaña debajo: el copy es del producto y el dato es del daemon.
  ///
  /// El recorrido de la lista NO se hace acá: lo hace [HealthAlert.listFrom],
  /// que es el único sitio del programa que decide qué es una alerta y qué es
  /// basura. Acá solo se le pone el copy encima. Dos recorridos con las mismas
  /// reglas escritas dos veces es cómo una se queda atrás sin que nadie lo note.
  static List<AppMessage> alertsFrom(Object? crudo) => <AppMessage>[
    for (final HealthAlert a in HealthAlert.listFrom(crudo))
      AppMessages.alertFromWire(a.wire, detail: a.detail),
  ];

  static AppMessage? _exitFrom(Object? crudo) {
    if (crudo is! String || crudo.isEmpty) return null;

    final AppMessage m = AppMessages.exitFromWire(crudo);
    // Salir por tu cuenta resuelve al mensaje vacío, y eso no se muestra. Se
    // devuelve null para que quien pregunte no tenga que saberlo.
    return m.isEmpty ? null : m;
  }
}
