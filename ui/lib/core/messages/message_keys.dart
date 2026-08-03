/// Las claves que el daemon manda por el cable, espejadas en Dart.
///
/// # Por qué existen como enum y no como String suelto
///
/// El daemon lo dice en su propio código, en `protocol.go`: *"Códigos y no
/// texto suelto porque la UI tiene que poder decidir qué pantalla mostrar sin
/// leer castellano. El texto viaja igual, para el log y para el diagnóstico que
/// el usuario copia al portapapeles."*
///
/// O sea que el reparto está decidido desde el otro lado: **el daemon manda la
/// clave, la UI pone el copy.** Estos enums son esa clave, y el catálogo de
/// `message_catalog.dart` es ese copy. Comparar strings a mano en cada pantalla
/// sería tener el contrato escrito en veinte sitios.
///
/// # Qué pasa con una clave que no conocemos
///
/// Cada `fromWire` devuelve `null` en vez de tirar. Un daemon más nuevo que la
/// UI puede mandar una clave que este enum no tiene, y la respuesta correcta a
/// eso jamás es romper la pantalla: el catálogo tiene un mensaje de reserva
/// para ese caso.
library;

/// Los avisos del módulo de exposición. Espejo de `domain.AlertKind`.
///
/// El orden es el del enum de Go, que además es el de gravedad percibida.
enum AlertKind {
  /// Un perfil del Firewall de Windows está apagado.
  firewallOff('firewall_off'),

  /// Las reglas del grupo Kanpachi no están como se aplicaron.
  rulesTampered('rules_tampered'),

  /// El router publica un puerto hacia esta máquina.
  routerMapping('router_mapping'),

  /// El propio juego dejó una regla que lo hace alcanzable sin Kanpachi.
  foreignRule('foreign_rule'),

  /// Una red de esta máquina pisa el rango fijo del vestíbulo.
  lobbyConflict('lobby_conflict'),

  /// Una expulsión no cerró sus dos capas.
  kickIncomplete('kick_incomplete'),

  /// El módulo de exposición no pudo comprobar lo que existe para comprobar.
  ///
  /// No dice que algo esté mal: dice que nadie está mirando.
  auditFailed('audit_failed');

  const AlertKind(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `kind` de `AlertView`.
  final String wire;

  static AlertKind? fromWire(String? wire) {
    for (final AlertKind kind in AlertKind.values) {
      if (kind.wire == wire) return kind;
    }
    return null;
  }
}

/// Por qué terminó la sesión anterior. Espejo de `domain.ExitReason`.
///
/// Sin esto, que te expulsen, que el host cierre, que desaparezca veinte
/// minutos y salir por tu cuenta se ven exactamente igual: la app vuelve a la
/// portada y no explica nada.
enum ExitReason {
  /// Saliste tú. El único que no tiene nada que explicar.
  user('user'),

  /// El host te sacó de la sala.
  kicked('kicked'),

  /// El host estuvo veinte minutos sin aparecer.
  hostGone('host_gone'),

  /// El host cerró la sala.
  roomClosed('room_closed'),

  /// No se llegó a entrar.
  failed('failed'),

  /// El túnel no volvió en diez minutos.
  tunnelLost('tunnel_lost');

  const ExitReason(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `last_exit` de `RoomView`.
  final String wire;

  static ExitReason? fromWire(String? wire) {
    for (final ExitReason reason in ExitReason.values) {
      if (reason.wire == wire) return reason;
    }
    return null;
  }
}

/// En qué anda la conexión con la sala. Espejo de `domain.ConnState`.
///
/// Los seis son del daemon y no de la UI, y por eso viven acá: la pantalla no
/// deduce si está reconectando, se lo dicen.
enum ConnState {
  /// Sin sala. Es el estado por defecto y no un error.
  idle('idle'),

  /// Resolviendo el código contra el vestíbulo.
  resolving('resolving'),

  /// Levantando el túnel.
  connecting('connecting'),

  /// Dentro y estable.
  connected('connected'),

  /// Dentro, con el túnel inestable.
  degraded('degraded'),

  /// El túnel se cayó y se está reintentando. Acotado a diez minutos.
  reconnecting('reconnecting');

  const ConnState(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `conn` de `RoomView`.
  final String wire;

  static ConnState? fromWire(String? wire) {
    for (final ConnState state in ConnState.values) {
      if (state.wire == wire) return state;
    }
    return null;
  }
}

/// Los códigos de error de la API local. Espejo de `protocol.Code`.
///
/// La regla de la casa aplicada acá: se decide por el CÓDIGO, jamás por el
/// texto. El `message` que acompaña al código viaja para el log y para el
/// diagnóstico que el usuario copia, no para que la UI lo muestre tal cual.
enum FailureCode {
  /// Los tres del transporte: parámetros que no encajan, sin saludo previo,
  /// mensaje por encima del tope.
  badRequest('bad_request'),
  unauthorized('unauthorized'),
  tooLarge('too_large'),

  /// Ya hay una sala abierta.
  busy('busy'),

  /// La operación necesita una sala y no hay.
  noRoom('no_room'),

  /// Solo el host puede hacer eso.
  notHost('not_host'),

  /// Ese juego no está en el catálogo.
  unknownGame('unknown_game'),

  /// Esa dirección no es de nadie presente.
  notAMember('not_a_member'),

  /// Expulsarse a uno mismo.
  selfKick('self_kick'),

  /// El perfil taparía uno que vino con la app.
  shadows('shadows'),

  /// Marcar verificado algo que no se jugó.
  notPlayed('not_played'),

  /// La expulsión se aplicó a medias.
  kickPartial('kick_partial'),

  /// No hay sala del arranque anterior.
  noPending('no_pending'),

  /// El nombre no cumple las reglas.
  badNickname('bad_nickname'),

  /// El invite ID no tiene forma de código.
  badCode('bad_code'),

  /// El perfil no pasa las invariantes.
  badProfile('bad_profile'),

  /// El adaptador de abajo falló.
  unavailable('unavailable'),

  /// Lo que no encaja en ninguno de arriba.
  internal('internal');

  const FailureCode(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `code` de `Error`.
  final String wire;

  static FailureCode? fromWire(String? wire) {
    for (final FailureCode code in FailureCode.values) {
      if (code.wire == wire) return code;
    }
    return null;
  }
}
