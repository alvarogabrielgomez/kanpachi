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
  auditFailed('audit_failed'),

  /// La compuerta NO está conteniendo, y reponerla no aguantó.
  ///
  /// Es la única que sale de una medición POR LA RED, y la única que ve el caso
  /// que ninguna otra puede ver: el filtro existe y no contiene nada. La levanta
  /// el socket propio del host y jamás el mensaje de un miembro.
  /// **El nombre de cable está congelado.** Dice `canary` porque el sensor
  /// que la levanta es el canario; lo que falla es la COMPUERTA, y por eso el
  /// nombre de acá dice eso. Cambiar la cadena rompería a toda ventana más
  /// vieja que el daemon que la sirve.
  gateLeaking('canary_leaking'),

  /// The room reopened by itself and its game could not be restored.
  ///
  /// The only one that talks about ports that are NOT open. The case is the host
  /// that restarts: the room comes back with its code and without its game, so
  /// people join and the server does not answer.
  gameLost('game_lost'),

  /// La cuarentena de base no está puesta entera: esta PC contesta en los
  /// puertos de servidor peligrosos (compartir archivos, Escritorio remoto)
  /// en cualquier red a la que se conecte.
  ///
  /// El aviso es el ESTADO y no un regaño por una vez: sale igual si el
  /// usuario decidió no tenerla, porque decir que no apaga los bloqueos y no
  /// el termómetro.
  quarantineOff('quarantine_off'),

  /// Hay miembros CONECTADOS en la sala y ninguno abrió su canal con este
  /// host.
  ///
  /// Es la única que no habla de exposición de más: habla de una sala que
  /// parece viva y no lo está. Solo la ve el host, que es quien tiene el
  /// oyente.
  noMemberChannels('no_member_channels'),

  /// A la subred de la sala le quedan pocas direcciones libres.
  ///
  /// Avisa antes de agotarse porque al agotarse el error lo ve el invitado que
  /// se queda fuera, y esta pantalla es la del host, que es quien puede hacer
  /// algo.
  roomAlmostFull('room_almost_full');

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

/// De qué es una regla de firewall ajena. Espejo de `domain.RuleClass`.
///
/// # Por qué no basta con el booleano `blocking`
///
/// El booleano decide si la sala se puede abrir; la clase decide qué se le
/// CUENTA al usuario. Son dos preguntas y solo una la contesta un `bool`: una
/// regla de más para el juego y una que entrega teclado y ficheros se resuelven
/// con acciones distintas y merecen textos distintos.
///
/// La decisión de si bloquea la toma el daemon y viaja hecha. Esto es solo el
/// nombre de lo que se encontró.
enum RuleClass {
  /// La dejó el instalador del juego o un diálogo de Windows.
  game('game'),

  /// Da teclado, pantalla y sistema de archivos.
  remoteControl('remote_control'),

  /// Alguien de fuera puso un permiso SOBRE un adaptador de Kanpachi.
  ///
  /// Lo que la hace peligrosa no es de quién es, es dónde está: deshace la
  /// promesa central en la misma capa que Kanpachi usa para conceder, y la
  /// compuerta no lo tapa porque los dos son permisos y conviven. El daemon la
  /// manda como bloqueante.
  onOurAdapter('on_our_adapter'),

  /// Ni una cosa ni la otra.
  other('other');

  const RuleClass(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `class` de `ForeignView`.
  final String wire;

  /// El valor de reserva cuando el daemon manda una clase que esta versión no
  /// conoce.
  ///
  /// Devuelve [RuleClass.remoteControl] y no [RuleClass.other], y la asimetría
  /// es deliberada: una clase desconocida vino de un daemon más nuevo que
  /// aprendió a detectar algo que esta UI no sabe nombrar, y de las dos formas
  /// de equivocarse, tratarla como lo peor sobreavisa mientras que tratarla
  /// como "otra" la esconde. La única que rompe la promesa es la segunda.
  static RuleClass fromWire(String? wire) {
    for (final RuleClass c in RuleClass.values) {
      if (c.wire == wire) return c;
    }
    return RuleClass.remoteControl;
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

  /// Dentro, con algún miembro llegando por el relay del seed. El túnel está
  /// entero: lo degradado es el camino hasta alguien, no la sala.
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

  /// La pregunta de la cuarentena de base sigue sin contestar, y quien llamó
  /// pidió que eso rechace. La ventana nunca lo pide, así que este código no
  /// le llega; la copia existe igual porque el catálogo es completo por
  /// construcción.
  quarantineUndecided('quarantine_undecided'),

  /// Ya hay una sala abierta.
  busy('busy'),

  /// Un cortafuegos que no es el nuestro deniega la entrada en los adaptadores
  /// de Kanpachi, así que abrir o entrar a una sala no serviría de nada.
  ///
  /// **No pasa en Windows hoy**, y está acá igual porque este enum es el espejo
  /// de `protocol.Code` y un espejo con huecos deja de avisar cuando el hueco
  /// importa. Lo produce un host de Linux con `ufw` o `firewalld` cerrado. Ver
  /// decisión 36.
  firewallBlocks('firewall_blocks'),

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

  /// El host no puede sondearse a sí mismo: eso lo pulsa alguien más.
  probeSelf('probe_self'),

  /// No se sabe dónde está el host para poder marcarle.
  probeNoHost('probe_no_host'),

  /// El registro contestó que ese código no existe.
  noSuchRoom('no_such_room'),

  /// El registro no contestó nada, así que no se pudo abrir ni entrar.
  ///
  /// Es el hermano de [noSuchRoom] y son dos códigos a propósito: allá hay
  /// respuesta y el código está mal, acá no hay respuesta y el código puede
  /// estar perfecto. Lo que hay que hacer es lo contrario en cada caso.
  noRegistry('no_registry'),

  /// Esta máquina todavía no tiene registro donde abrir salas.
  ///
  /// Es el hermano de [seedPassword] y son dos a propósito: acá falta elegir el
  /// servidor, allá falta la contraseña de uno ya elegido. Llevan a dos
  /// pantallas distintas.
  noOwnSeed('no_own_seed'),

  /// El código que pegaron no dice en qué servidor vive esa sala.
  ///
  /// El tercero de la familia, y hacen falta los tres. [noOwnSeed] es que esta
  /// máquina no eligió dónde hospedar, [seedPassword] es que el servidor
  /// elegido pide credencial, y este es que lo pegado no identifica una sala:
  /// los mismos ocho caracteres existen en tantas salas como registros haya.
  ///
  /// **Antes llegaba como `internal`**, o sea con la copia que dice que
  /// Kanpachi se rompió y que hay que reiniciar la app. Nada se rompió: falta
  /// media línea en lo que pegaron. Medido corriendo el CLI contra el daemon.
  seedMissing('seed_missing'),

  /// Ese registro pide password para HOSPEDAR y esta máquina no tiene con qué.
  ///
  /// Cubre los tres casos a la vez: nunca se escribió ninguno, el guardado
  /// caducó, y el operador cambió el password y botó a todos. Son uno solo
  /// porque lo que hay que hacer es idéntico, y porque el registro se niega a
  /// decir cuál fue: distinguirlos solo le regalaría información a quien esté
  /// probando contraseñas.
  ///
  /// **Entrar a una sala nunca lo produce.**
  seedPassword('seed_password'),

  /// El usuario canceló la operación mientras corría.
  canceled('canceled'),

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

/// Si la compuerta del adaptador virtual está puesta. Espejo de
/// `domain.GateState`.
///
/// # Por qué "sin comprobar" no es "ausente"
///
/// Ausente es un hecho: se miró y no está, y con eso la contención degrada a lo
/// que hacen las reglas del Firewall de Windows, que es lo que el producto tenía
/// antes de que la compuerta existiera. Nada nuevo queda abierto.
///
/// Sin comprobar es ceguera, y se pinta distinto a propósito. La misma doctrina
/// que [AlertKind.auditFailed]: una medición que no ocurrió jamás se enseña como
/// una que salió bien.
enum GateState {
  /// Puesta. Todo lo que no esté permitido en el adaptador está cerrado.
  present('present'),

  /// Se midió y no está.
  absent('absent'),

  /// No se pudo medir.
  unknown('unknown');

  const GateState(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `gate` de `ExposureView`.
  final String wire;

  /// Lo que no se reconoce cae en [GateState.unknown], y no en `absent`.
  ///
  /// Es el lado seguro por dos motivos: una versión vieja de la UI contra un
  /// daemon nuevo dice "no pude comprobarlo", que es verdad, en vez de afirmar
  /// una ausencia que no midió.
  static GateState fromWire(String? wire) {
    for (final GateState state in GateState.values) {
      if (state.wire == wire) return state;
    }
    return GateState.unknown;
  }
}

/// El veredicto del sondeo desde otra máquina.
///
/// # Por qué "no se pudo alcanzar" y "cerrado" son valores distintos
///
/// Porque "no contesta nadie" se ve exactamente igual con la máquina blindada
/// que con la máquina apagada, y confundirlos sería pintar de verde una PC que
/// no se midió. La referencia es lo que los separa: el canal de la sala tiene
/// que contestar, y hasta que lo haga el resto del sondeo no afirma nada.
enum ProbeVerdict {
  /// Algo que nadie pidió contestó. Prueba POSITIVA de exposición.
  leaky('leaky'),

  /// Ni el canal de la sala contestó, así que esto no prueba nada.
  unreachable('unreachable'),

  /// El canal contesta y nada de lo prohibido lo hace.
  sealed('sealed'),

  /// No se midió.
  blind('blind');

  const ProbeVerdict(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `verdict` de `ProbeView`.
  final String wire;

  /// Lo que no se reconoce cae en [ProbeVerdict.blind], que es el único valor
  /// que no afirma nada sobre la otra máquina.
  static ProbeVerdict fromWire(String? wire) {
    for (final ProbeVerdict v in ProbeVerdict.values) {
      if (v.wire == wire) return v;
    }
    return ProbeVerdict.blind;
  }
}

/// La conclusión de una ronda del canario. Espejo de `domain.CanaryVerdict`.
///
/// # Las dos fuentes no valen lo mismo, y estos valores lo dicen
///
/// El host abre un oyente que la compuerta tiene que bloquear y le pide a TODOS
/// los de la sala que lo marquen. Lo que concluye sale de su PROPIO socket, no
/// de lo que diga nadie: un mensaje se puede mentir, que un paquete haya llegado
/// hasta el oyente no.
///
/// Por eso [CanaryVerdict.clean] se llama así y no "bloqueando": describe que no
/// hay evidencia de fuga, que es más débil y es lo que de verdad se sabe. Un
/// miembro que se quede quieto produce ese mismo estado.
enum CanaryVerdict {
  /// El paquete CRUZÓ. La compuerta no está conteniendo el adaptador. Es lo
  /// único que se afirma con certeza.
  leaking('leaking'),

  /// No hay evidencia de fuga y alguien dice haber marcado.
  clean('clean'),

  /// Nadie contestó, o no había a quién preguntarle. No dice nada de la
  /// compuerta.
  unconfirmed('unconfirmed'),

  /// Un miembro dijo que llegó y al canario no lo tocó nadie. No acusa a nadie
  /// por sí solo: también sale de una carrera entre el informe y el cierre.
  mismatch('mismatch'),

  /// No se comprobó.
  blind('blind');

  const CanaryVerdict(this.wire);

  /// La cadena exacta que viaja en el JSON, campo `verdict` de `CanaryView`.
  final String wire;

  /// Lo que no se reconoce cae en [CanaryVerdict.blind], jamás en
  /// [CanaryVerdict.clean]. Un valor que esta versión no conoce no puede leerse
  /// como que no hay evidencia de fuga.
  static CanaryVerdict fromWire(String? wire) {
    for (final CanaryVerdict v in CanaryVerdict.values) {
      if (v.wire == wire) return v;
    }
    return CanaryVerdict.blind;
  }
}

/// Por qué un puerto está en la lista del sondeo, que es lo mismo que decir qué
/// prueba su respuesta.
enum ProbeKind {
  /// El canal de la sala: el único que TIENE que contestar.
  reference('reference'),

  /// Lo que NO tiene que contestar. Una respuesta acá es la alarma.
  forbidden('forbidden'),

  /// Un puerto que el juego activo pidió. Informativo: que calle no prueba
  /// nada, porque el juego puede estar cerrado.
  game('game');

  const ProbeKind(this.wire);

  final String wire;

  /// Lo desconocido cae en [ProbeKind.forbidden], que es el lado RUIDOSO. Una
  /// clase nueva que se pintara como puerto de juego haría que su respuesta
  /// pasara por normal, que es justo lo contrario de lo que hace falta.
  static ProbeKind fromWire(String? wire) {
    for (final ProbeKind k in ProbeKind.values) {
      if (k.wire == wire) return k;
    }
    return ProbeKind.forbidden;
  }
}

/// Lo que contestó un puerto.
enum ProbeOutcome {
  /// Hubo apretón de manos: el firewall lo dejó pasar y hay algo escuchando.
  answered('answered'),

  /// Rebotó con un RST. El paquete llegó igual.
  refused('refused'),

  /// No contestó nadie. Medido en Windows: no distingue "bloqueado" de "no hay
  /// nada escuchando", y por eso el silencio solo, sin referencia, no prueba
  /// casi nada.
  silent('silent'),

  /// Esta máquina no pudo ni preguntar. No dice nada de la otra.
  failed('failed');

  const ProbeOutcome(this.wire);

  final String wire;

  /// Lo desconocido cae en [ProbeOutcome.failed] y jamás en `silent`: un
  /// resultado que no se sabe leer no puede contarse como "está cerrado".
  static ProbeOutcome fromWire(String? wire) {
    for (final ProbeOutcome o in ProbeOutcome.values) {
      if (o.wire == wire) return o;
    }
    return ProbeOutcome.failed;
  }
}

/// La cuarentena de base tal como se MIDIÓ, espejo de `quarantineVerdictName`
/// en `view.go` del daemon.
enum QuarantineVerdict {
  /// Todas las reglas puestas, activas y diciendo lo que se escribió.
  applied('applied'),

  /// Parte está y parte no: reglas ausentes, deshabilitadas o editadas.
  partial('partial'),

  /// No hay ni una regla puesta.
  absent('absent'),

  /// No se pudo medir. NO es "apagada": es que nadie pudo mirar.
  unknown('unknown');

  const QuarantineVerdict(this.wire);

  /// La cadena exacta del campo `verdict` de `QuarantineView`.
  final String wire;

  /// Lo que no se reconoce cae en [QuarantineVerdict.unknown], que es el único
  /// que no afirma nada: leerlo como "puesta" pintaría protección que nadie
  /// midió, y como "apagada" asustaría por un valor nuevo del daemon.
  static QuarantineVerdict fromWire(String? wire) {
    for (final QuarantineVerdict v in QuarantineVerdict.values) {
      if (v.wire == wire) return v;
    }
    return QuarantineVerdict.unknown;
  }
}

/// La RESPUESTA del usuario sobre la cuarentena, espejo de
/// `quarantineDecisionName` en `view.go`. Viaja al lado de la medición porque
/// el interruptor dibuja las dos: qué hay puesto, y si la pregunta sigue
/// debida.
enum QuarantineDecision {
  yes('yes'),
  no('no'),

  /// Nadie contestó todavía. No es "no": es lo que hace que la pregunta se
  /// haga exactamente una vez.
  undecided('undecided');

  const QuarantineDecision(this.wire);

  final String wire;

  /// Lo desconocido cae en [QuarantineDecision.undecided]: inventar un "no"
  /// callaría la pregunta, e inventar un "sí" afirmaría un consentimiento que
  /// nadie dio.
  static QuarantineDecision fromWire(String? wire) {
    for (final QuarantineDecision d in QuarantineDecision.values) {
      if (d.wire == wire) return d;
    }
    return QuarantineDecision.undecided;
  }
}

/// Which thing the user asked for. One per button that can fail.
///
/// Lives here, next to the wire enums, and not in the session feature: this is
/// a key into the message catalog, and `core/messages` cannot depend on a
/// feature without turning the dependency arrow around.
///
/// It drives the COPY. "No se pudo abrir la sala" and "no se pudo renovar el
/// código" are different sentences, and one generic "algo salió mal" is the
/// message that teaches people to stop reading messages.
enum FailedAction {
  createRoom,
  joinRoom,
  leaveRoom,
  activateProfile,
  kickMember,
  rotateInviteCode,
  renameRoom,
  resumeRoom,
  discardSavedRoom,
  forgetLastRoom,
  reapplyProtection,
  probeHost,
  loadCatalog,
  saveProfile,
  importCatalog,
  exportCatalog,
  suspendForeignRules,
  quarantine,
  setNickname,
  quit,
}
