import 'package:kanpachi_ui/core/messages/app_message.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';

/// El catálogo: una clave del daemon entra, el texto del producto sale.
///
/// # Qué problema resuelve
///
/// El copy vivía escrito a mano en cada pantalla que lo necesitaba, y eso tiene
/// tres consecuencias que se notan tarde. La misma situación se explicaba
/// distinto en dos sitios. Cambiar una frase obligaba a buscarla por el árbol.
/// Y no había forma de responder la pregunta que importa: **¿hay alguna
/// situación que el daemon sepa reportar y para la que no tengamos texto?**
///
/// Con el catálogo, esa pregunta es un test. Y como cada método es un `switch`
/// sobre un enum sin `default`, agregar un valor a `AlertKind` deja de compilar
/// hasta que alguien escriba su texto. No es un recordatorio: es el compilador.
///
/// # Dónde vive el copy de verdad
///
/// En `docs/05-ui.md`, que es la fuente. Ese documento ya traía los textos
/// redactados en prosa antes de que existiera este archivo, así que esto es un
/// traslado y no una invención. La regla del proyecto aplica igual: si acá
/// cambia una frase, cambia ahí en el mismo commit.
///
/// # Lo que NO hace
///
/// No decide cuándo mostrar nada, no guarda estado y no pinta. Solo traduce.
/// Quien decide es el cubit y quien pinta es un widget.
abstract final class AppMessages {
  /// El aviso que corresponde a una alerta del módulo de exposición.
  ///
  /// El `detail` es el dato concreto que aportó el daemon y se acompaña, jamás
  /// sustituye al cuerpo: el texto del producto se escribe acá y no depende de
  /// cómo redacte el daemon.
  static AppMessage alert(AlertKind kind, {String? detail}) =>
      _alert(kind).withDetail(detail);

  static AppMessage _alert(AlertKind kind) => switch (kind) {
    AlertKind.firewallOff => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Tu Firewall de Windows está apagado',
      body:
          'Kanpachi se apoya en él para que nadie de la sala alcance '
          'tu PC. Sin él, no puede protegerte. ',
      hint: 'Cómo activarlo',
    ),
    AlertKind.rulesTampered => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Algo está borrando las reglas de Kanpachi',
      body:
          'Suele ser un antivirus. Mientras pase, la sala puede no '
          'funcionar.',
    ),
    AlertKind.routerMapping => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Tu router tiene un puerto abierto hacia internet',
      body:
          'Kanpachi no lo necesita: hace el túnel sin abrir nada. '
          'Mientras siga así, cualquiera en internet llega a ese '
          'puerto. ',
      hint: 'Cómo cerrarlo',
    ),
    AlertKind.foreignRule => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'El juego dejó una regla que lo hace visible en tu red',
      body:
          'Quien esté en tu red de casa llega al juego sin pasar por '
          'Kanpachi, y seguirá llegando aunque saques a alguien de la '
          'sala. Se puede desactivar mientras dure la sala y se '
          'restaura sola al salir.',
    ),
    AlertKind.lobbyConflict => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Tu red usa el mismo rango que el vestíbulo de Kanpachi',
      body:
          'Entrar a salas de otros puede fallar. Es el único rango que '
          'Kanpachi no puede mover, porque tiene que ser el mismo en '
          'todas las máquinas.',
    ),
    AlertKind.kickIncomplete => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'La expulsión quedó a medias',
      body:
          'Ya no está en la lista de la sala y su máquina puede seguir '
          'autorizada. Renueva el código para cerrarle la puerta del '
          'todo.',
    ),

    // La que no se parece a ninguna: las demás cuentan un hallazgo, esta
    // cuenta que no hubo con qué mirar. Sin decirlo, una auditoría caída y
    // una máquina impecable se ven idénticas, porque las dos pintan verde.
    AlertKind.auditFailed => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Kanpachi no pudo comprobar cómo está tu PC',
      body:
          'No quiere decir que algo esté mal: quiere decir que nadie '
          'lo está vigilando. Reinicia Kanpachi y si sigue igual, copia '
          'el diagnóstico.',
    ),
    // El texto dice lo que la alarma SIGNIFICA de verdad, y por eso habla en
    // pasado del intento. Kanpachi ya repuso la protección una vez sola,
    // callado, y esto aparece porque no aguantó. Un texto que solo dijera
    // "pulsa el botón" dejaría al usuario creyendo que nada pasó hasta que
    // él hiciera algo.
    AlertKind.canaryLeaking => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Tu protección no está conteniendo',
      body:
          'Alguien de la sala alcanzó un puerto de esta PC que tenía '
          'que estar cerrado. Kanpachi ya intentó reponer la protección '
          'y no aguantó.',
      hint:
          'Vuelve a aplicarla. Si el problema sigue, sal de la sala: '
          'eso cierra todo lo que Kanpachi abrió.',
    ),
    // The only one that warns about ports that are NOT open, which is why the
    // text opens by saying the room is fine. Without that first sentence the
    // title reads as the room being broken instead of what it is: the room
    // works, no game is set, and whoever joins will not be able to play.
    AlertKind.gameLost => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'La sala volvió sin su juego',
      body:
          'La sala está abierta y con el mismo código de siempre. Lo que '
          'no se pudo reponer es el juego que tenía activo, así que no '
          'hay ningún puerto abierto y quien entre no va a poder jugar.',
      hint: 'Vuelve a elegir el juego.',
    ),
  };

  /// Lo que se le cuenta al usuario sobre una regla de firewall que Kanpachi no
  /// creó.
  ///
  /// Son textos distintos porque son problemas distintos, y tratarlos igual era
  /// el bug: una regla de más para el juego molesta, y una de escritorio remoto
  /// entrega la máquina. La segunda BLOQUEA abrir la sala, y el daemon manda ese
  /// veredicto ya tomado.
  ///
  /// El `gameName` solo aparece en la del juego. En la de control remoto sobra:
  /// lo que importa ahí no es qué juego dejó la regla sino qué programa la
  /// tiene, y ese va en el `detail`.
  static AppMessage foreignRule(
    RuleClass kind, {
    String? gameName,
    String? detail,
  }) => _foreignRule(kind, gameName).withDetail(detail);

  static AppMessage _foreignRule(
    RuleClass kind,
    String? gameName,
  ) => switch (kind) {
    // **Habla de la red de casa y NO de la sala**, y esa corrección importa.
    //
    // Decía que la regla dejaba el juego alcanzable "por toda la sala, sin
    // pasar por el control de Kanpachi: expulsar a alguien no lo tapa". Eso es
    // justo lo que la compuerta existe para que sea falso: es un bloqueo duro
    // de WFP acotado al adaptador virtual, y en WFP un Block gana sobre
    // cualquier permiso ajeno. Dentro de la sala esta regla no consigue nada, y
    // a un expulsado lo sigue tapando la compuerta.
    //
    // Lo que sí queda al descubierto es todo lo demás: la compuerta está
    // acotada al adaptador virtual a propósito, y esa acotación es lo que
    // impide dejar al usuario sin su red de casa. Ahí esta regla vale, y la
    // dejó puesta el instalador del juego para siempre.
    //
    // Sobrestimar un aviso no lo hace más seguro: enseña a no leerlos.
    RuleClass.game => AppMessage(
      severity: MessageSeverity.warn,
      title: '${gameName ?? 'El juego'} dejó una regla en tu firewall',
      body:
          'La puso su instalador, y deja el juego alcanzable desde tu red de '
          'casa y desde cualquier otra a la que conectes este PC, con Kanpachi '
          'cerrado. Dentro de la sala no cambia nada: ahí manda Kanpachi. Se '
          'puede desactivar mientras juegas y se devuelve al salir.',
    ),

    // La única que impide abrir la sala, y por eso no ofrece "dejar así"
    // como una opción equivalente a la otra. El texto nombra lo que se
    // entrega en vez de decir "riesgo de seguridad", porque lo primero se
    // entiende y lo segundo se despacha.
    RuleClass.remoteControl => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Un programa de control remoto está abierto en tu firewall',
      body:
          'Quien entre a la sala puede llegar a él y quedarse con tu '
          'teclado, tu pantalla y tus archivos, aunque lo expulses '
          'después: el código de invitación no es un secreto. Hay que '
          'resolverlo antes de abrir la sala. Se puede desactivar '
          'mientras juegas y se devuelve al salir.',
    ),

    // La otra que impide abrir la sala. Lo que la hace peligrosa no es de
    // quién es, es DÓNDE está, y por eso el texto habla del sitio y no del
    // programa: puede no haber ninguno.
    RuleClass.onOurAdapter => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Algo abrió tu red de Kanpachi por fuera de Kanpachi',
      body:
          'Hay un permiso en tu firewall puesto sobre la red virtual que '
          'Kanpachi crea, y no lo puso Kanpachi. Mientras esté, cualquiera '
          'de la sala llega a esa máquina sin pasar por el control de '
          'Kanpachi, y expulsarlo no lo tapa. Hay que resolverlo antes de '
          'abrir la sala. Se puede desactivar mientras juegas y se devuelve '
          'al salir.',
    ),

    // Se muestra igual en vez de callarse. Kanpachi la encontró buscando lo
    // que abre la máquina, así que decir "hay algo que no sé clasificar" es
    // más honesto que no decir nada.
    RuleClass.other => const AppMessage(
      severity: MessageSeverity.neutral,
      title: 'Hay una regla en tu firewall que Kanpachi no puso',
      body:
          'No es del juego ni de un programa de control remoto '
          'conocido. Se puede desactivar mientras dure la sala y se '
          'devuelve al salir.',
    ),
  };

  /// Por qué volviste a la portada, si la sesión anterior terminó sola.
  ///
  /// Las cuatro del medio llevan la misma coletilla sobre lo que se cerró, y es
  /// a propósito: responde la pregunta que la gente se hace sola, que es si
  /// quedó algo abierto.
  static AppMessage exit(ExitReason reason) => switch (reason) {
    // Saliste tú. No hay nada que explicar.
    ExitReason.user => AppMessage.none,
    ExitReason.kicked => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'El host te sacó de la sala.',
    ),
    ExitReason.roomClosed => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'El host cerró la sala.',
    ),
    ExitReason.hostGone => const AppMessage(
      severity: MessageSeverity.neutral,
      body:
          'El host llevaba veinte minutos sin aparecer, así que '
          'Kanpachi cerró todo lo que había abierto.',
    ),
    ExitReason.tunnelLost => const AppMessage(
      severity: MessageSeverity.neutral,
      body:
          'La conexión con la sala no volvió. Kanpachi lo intentó '
          'durante diez minutos y cerró todo lo que había abierto. '
          'Vuelve a pegar el código cuando tengas red.',
    ),
    ExitReason.failed => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'No se pudo entrar a la sala.',
    ),
  };

  /// Qué se le dice al usuario cuando una operación de la API local falla.
  ///
  /// Varios códigos comparten texto a propósito. `bad_request`, `too_large` e
  /// `internal` describen cosas distintas para quien lee el log y la misma cosa
  /// para quien está delante: algo del programa falló y no es culpa suya.
  /// Diferenciarlos en pantalla sería exponer la estructura del daemon a quien
  /// no puede hacer nada con ella.
  static AppMessage failure(FailureCode code) => switch (code) {
    FailureCode.badCode => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Ese código no parece completo. Son 8 caracteres, revisa que '
          'se copió entero.',
    ),
    FailureCode.badNickname => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Ese nombre no se puede usar. Van entre 2 y 20 caracteres, '
          'sin símbolos raros.',
    ),
    FailureCode.unavailable => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'No hay conexión con el servidor de encuentro. Revisa tu '
          'internet, si persiste avisa en el grupo.',
    ),
    FailureCode.busy => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'Ya estás en una sala. Sal de esa antes de abrir otra.',
    ),
    FailureCode.noRoom => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'Esa acción necesita una sala abierta.',
    ),
    FailureCode.notHost => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'Eso solo lo puede hacer quien abrió la sala.',
    ),
    FailureCode.unknownGame => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Ese juego no está en el catálogo. Elígelo de la biblioteca '
          'o créale un perfil.',
    ),
    FailureCode.notAMember => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'Esa persona ya no está en la sala.',
    ),
    FailureCode.selfKick => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'No te puedes sacar a ti mismo. Para irte, sal de la sala.',
    ),
    FailureCode.shadows => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Ya hay un perfil con ese nombre entre los que vinieron con '
          'la app. Ponle otro para no taparlo.',
    ),
    FailureCode.notPlayed => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Un perfil se marca verificado después de jugarlo con '
          'alguien más, no antes.',
    ),
    FailureCode.badProfile => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Ese perfil pide algo que Kanpachi no abre nunca. Lo peor '
          'que puede pasar es que ese juego no conecte.',
    ),
    FailureCode.probeSelf => const AppMessage(
      severity: MessageSeverity.neutral,
      body:
          'Tu PC no se puede comprobar a sí misma. Pídele a alguien de '
          'la sala que abra esta pantalla y pulse el botón.',
    ),
    FailureCode.probeNoHost => const AppMessage(
      severity: MessageSeverity.neutral,
      body:
          'Todavía no se sabe dónde está el host, así que no hay a qué '
          'marcarle. Prueba en un momento.',
    ),
    FailureCode.kickPartial => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'La expulsión quedó a medias. Renueva el código para '
          'cerrarle la puerta del todo.',
    ),
    FailureCode.noPending => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'Esta máquina no tiene ninguna sala guardada.',
    ),
    // Es lo primero que se comprueba al pegar un código, antes de levantar
    // nada, así que llega en el primer segundo. El texto tiene que servir para
    // las dos causas que producen la misma respuesta.
    FailureCode.noSuchRoom => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Ese código no existe. Puede estar mal copiado, o la sala se cerró '
          'y el host tiene que pasarte uno nuevo.',
    ),
    // El hermano del de arriba, con el texto al revés en lo que importa: allá
    // hay que conseguir otro código, acá el que se tiene puede estar perfecto.
    // Sirve para crear y para entrar, que fallan por lo mismo.
    FailureCode.noRegistry => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'El servidor de encuentro no responde, así que ahora mismo no se '
          'puede crear una sala ni entrar a una. Prueba de nuevo en un rato.',
    ),
    // Los dos que se arreglan yendo a una pantalla, y por eso el texto no dice
    // «prueba otra vez»: no hay nada que reintentar hasta que alguien elija.
    FailureCode.noOwnSeed => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Todavía no elegiste en qué servidor abres tus salas. Entrar a la '
          'sala de alguien no lo elige por ti.',
    ),
    // Se arregla pidiendo el enlace otra vez, así que el texto enseña la forma
    // completa: quien dictó el código por teléfono se quedó con la mitad.
    FailureCode.seedMissing => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'A ese código le falta el servidor. Los mismos ocho caracteres son '
          'salas distintas en servidores distintos, así que hace falta el '
          'enlace entero o la forma CÓDIGO@servidor.',
    ),
    // El fallo de esta lista que se arregla escribiendo algo. El texto
    // no dice si el password nunca se puso, si caducó o si lo cambiaron,
    // porque el registro tampoco lo dice y lo que hay que hacer es lo mismo.
    FailureCode.seedPassword => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Ese servidor pide una contraseña para poder abrir salas en él. '
          'Entrar a una sala nunca la pide.',
    ),
    // Neutral y no advertencia: no falló nada, se pulsó un botón.
    FailureCode.canceled => const AppMessage(
      severity: MessageSeverity.neutral,
      body: 'Se canceló. Todo lo que se había abierto quedó cerrado.',
    ),

    // Los cuatro que el usuario no puede accionar. Ver el doc de arriba.
    FailureCode.badRequest ||
    FailureCode.tooLarge ||
    FailureCode.unauthorized ||
    FailureCode.internal => const AppMessage(
      severity: MessageSeverity.warn,
      body:
          'Algo falló dentro de Kanpachi. Reinicia la app, y si sigue '
          'pasando copia el diagnóstico y avisa en el grupo.',
    ),
  };

  /// La línea de estado de la conexión con la sala.
  ///
  /// Los tres primeros no llevan texto porque no son un aviso: la pantalla ya
  /// cuenta lo que está pasando con su propio contenido, y una línea que diga
  /// "conectando" encima de una pantalla que dice "conectando" es ruido.
  ///
  /// `reconnecting` está acotado a diez minutos. Pasado ese plazo se sale de la
  /// sala y el texto pasa a ser el de [ExitReason.tunnelLost]. Que la ruedita
  /// tenga final vale escribirlo: una ruedita eterna es la forma más común de
  /// que una app mienta.
  static AppMessage connection(ConnState state) => switch (state) {
    ConnState.idle ||
    ConnState.resolving ||
    ConnState.connecting ||
    ConnState.connected => AppMessage.none,
    // Degradado NO es reintentando, y el texto viejo decía las dos cosas mal:
    // ni la conexión está inestable ni hay nada reintentándose. Degradado es
    // que alguien llega por el relay del seed, con el túnel entero en pie.
    // Decirlo así es lo que evita que se culpe al juego de una latencia que es
    // de la red.
    ConnState.degraded => const AppMessage(
      severity: MessageSeverity.warn,
      body: 'Alguien llega por el relay, la partida va más lenta',
    ),
    ConnState.reconnecting => const AppMessage(
      severity: MessageSeverity.warn,
      body: 'Reconectando…',
    ),
  };

  /// La segunda fila de la pantalla de exposición: qué pasa con todo lo que no
  /// está en la lista de arriba.
  ///
  /// # Por qué esta fila existe
  ///
  /// Porque la lista de puertos abiertos, sola, es cierta y engañosa a la vez:
  /// enumera lo que Kanpachi abrió sin decir nada de la puerta de al lado. Es
  /// esta fila la que convierte "lo que Kanpachi abrió" en "lo que hay
  /// abierto", y por eso vale tanto como la lista.
  static AppMessage gate(GateState state) => switch (state) {
    GateState.present => const AppMessage(
      severity: MessageSeverity.done,
      title: 'Todo lo demás está cerrado',
      body:
          'Nadie de la sala puede alcanzar ningún otro puerto de esta '
          'PC por la red de Kanpachi, ni aunque algún programa haya '
          'dejado su propio permiso puesto.',
    ),
    GateState.absent => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Solo está abierto lo de arriba, y no está cerrado el resto',
      body:
          'Los puertos de la lista son los que Kanpachi abrió. Lo que '
          'otro programa haya abierto por su cuenta sigue abierto para '
          'la gente de la sala.',
      hint: 'Salir de la sala y volver a entrar suele reponerlo.',
    ),
    GateState.unknown => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'Kanpachi no pudo leer lo que tiene puesto',
      body:
          'Esto no dice que algo esté mal: dice que nadie está '
          'mirando. La lista de arriba puede estar incompleta.',
    ),
  };

  /// Un puerto que Kanpachi pidió abrir y que el sistema no tiene puesto.
  ///
  /// No es un detalle técnico: es la explicación de por qué un amigo se queda
  /// fuera, y sin ella el síntoma es "a mí no me conecta" sin nada que mirar.
  static AppMessage portNotApplied() => const AppMessage(
    severity: MessageSeverity.warn,
    title: 'Este puerto tenía que estar abierto y no lo está',
    body:
        'Quien intente entrar por acá no va a poder. Suele arreglarse '
        'volviendo a elegir el juego.',
  );

  /// El resultado del sondeo desde otra máquina.
  ///
  /// # Por qué "no se pudo alcanzar" no dice "está cerrado"
  ///
  /// Porque no lo sabe. Está MEDIDO que en Windows un puerto callado no
  /// distingue "bloqueado" de "no hay nada escuchando", así que sin una
  /// respuesta que llegue, el silencio de una PC blindada y el de una PC
  /// apagada son idénticos. Decir "cerrado" ahí sería afirmar lo que no se
  /// midió, que es la misma doctrina que [GateState.unknown].
  ///
  /// `leaks` son los puertos que contestaron sin que nadie los pidiera, y van
  /// dentro del cuerpo: "hay algo abierto" sin decir qué manda al usuario a
  /// buscar a ciegas.
  static AppMessage probe(
    ProbeVerdict verdict, {
    List<String> leaks = const <String>[],
  }) => switch (verdict) {
    ProbeVerdict.leaky => AppMessage(
      severity: MessageSeverity.warn,
      title: 'Desde otra PC se llega a algo que Kanpachi no abrió',
      body:
          'Contestaron: ${leaks.join(', ')}. Eso significa que quien '
          'esté en la sala puede alcanzar ese programa de esta PC. '
          'Kanpachi no lo abrió, así que lo dejó puesto otra cosa.',
      hint: 'Revisa las reglas ajenas antes de seguir jugando.',
    ),
    ProbeVerdict.unreachable => const AppMessage(
      severity: MessageSeverity.warn,
      title: 'No se pudo alcanzar esa PC',
      body:
          'Ni siquiera contestó el canal de la sala, que tendría que '
          'contestar siempre. Esto NO quiere decir que esté cerrada: '
          'quiere decir que no se pudo comprobar nada.',
      hint: 'Suele ser que el túnel se cayó. Prueba otra vez.',
    ),
    ProbeVerdict.sealed => const AppMessage(
      severity: MessageSeverity.done,
      title: 'Desde acá no se alcanza nada más de esa PC',
      body:
          'Contestó el canal de la sala, así que la comprobación llegó '
          'de verdad, y ninguno de los puertos peligrosos contestó.',
      hint:
          'Se prueban puertos TCP conocidos, no todos: una herramienta '
          'con la configuración cambiada, o que hable UDP como Parsec, no '
          'se ve desde acá.',
    ),
    ProbeVerdict.blind => const AppMessage(
      severity: MessageSeverity.neutral,
      title: 'Todavía no se ha probado',
      body:
          'Esta es la única comprobación que sale a la red de verdad, y '
          'la tiene que pulsar alguien.',
    ),
  };

  /// El host se fue y el juego corría en su PC.
  ///
  /// Lleva el nombre dentro, así que es de los pocos que reciben un dato. El
  /// respaldo no es un hueco vacío: sin nombre, "El host" dice lo mismo.
  static AppMessage hostLeft(String? hostName) => AppMessage(
    severity: MessageSeverity.neutral,
    title: '${hostName ?? 'El host'} salió de la sala',
    body:
        'El juego corría en su PC, así que no hay a qué conectarse '
        'hasta que vuelva. La sala sigue en pie.',
  );

  /// Esta máquina está volviendo a pedir credencial.
  ///
  /// Le gana al cartel de que el host se fue, y por eso lleva `pulse`: son unos
  /// diez segundos en los que la sala parece congelada, y sin decirlo se leen
  /// como que la app se colgó.
  static const AppMessage rejoining = AppMessage(
    severity: MessageSeverity.neutral,
    title: 'Volviendo a entrar a la sala',
    body:
        'El host se reinició y ya no reconocía esta máquina, así que se le '
        'está pidiendo entrada otra vez. Tarda unos segundos y no hay nada '
        'que hacer.',
  );

  /// Esta máquina está volviendo a una sala en la que estuvo.
  ///
  /// # No es [rejoining], y la diferencia importa
  ///
  /// Aquello es pedir credencial DESDE DENTRO de una sala viva, dura segundos y
  /// no hay nada que hacer. Esto es no estar en ninguna sala y seguir
  /// intentándolo cada cinco minutos, sin tope, y **sí hay algo que hacer**: por
  /// eso lleva el botón de salir al lado.
  ///
  /// Que el registro no conteste manda sobre el motivo del último intento,
  /// porque son dos sitios distintos donde mirar: un host dormido se arregla
  /// esperando y un registro caído no es asunto de quien espera.
  static AppMessage returning({
    required String room,
    required String code,
    required Duration nextIn,
    required int attempts,
    bool seedDown = false,
  }) {
    final String cuando = nextIn > Duration.zero
        ? 'Se reintenta en ${_enPocasPalabras(nextIn)}.'
        : 'Reintentando ahora mismo.';
    final String motivo = seedDown
        ? 'El servidor de encuentro no contesta, así que todavía no se sabe '
              'nada de la sala.'
        : 'Se entra sola en cuanto el host vuelva a estar disponible.';
    return AppMessage(
      severity: MessageSeverity.neutral,
      title: 'Volviendo a ${room.isEmpty ? code : room}',
      body: '$motivo $cuando',
      hint: attempts > 1 ? 'Van $attempts intentos.' : null,
    );
  }

  /// Una duración en las palabras que usaría una persona. Sin segundos por
  /// debajo del minuto: un contador al segundo invita a mirarlo, y no hay nada
  /// que mirar.
  static String _enPocasPalabras(Duration d) {
    if (d.inMinutes < 1) return 'menos de un minuto';
    if (d.inMinutes == 1) return 'un minuto';
    return '${d.inMinutes} minutos';
  }

  /// El registro perdió la entrada de una sala que sigue abierta.
  ///
  /// No es una caída del seed: esa se cura sola y no afirma que el código haya
  /// muerto. Acá el registro contestó explícitamente que no lo conoce, así que
  /// la única salida que conserva la partida es renovarlo.
  static const AppMessage codeLost = AppMessage(
    severity: MessageSeverity.warn,
    title: 'El código de esta sala dejó de funcionar',
    body:
        'La sala sigue funcionando para quienes ya están dentro, pero nadie '
        'nuevo puede entrar con ese código. Renuévalo para abrir una puerta '
        'nueva sin cortar la partida.',
  );

  /// No se pudo hablar con el servicio de Kanpachi.
  ///
  /// **No es un `FailureCode`,** y la diferencia importa: aquellos son códigos
  /// que MANDÓ el daemon, o sea que hubo daemon. Este es que no hubo con quién
  /// hablar, así que no puede venir de ningún sitio salvo de acá.
  ///
  /// Dice qué hacer y no solo qué pasó, porque la causa habitual tiene arreglo
  /// y el usuario no tiene forma de adivinarlo desde una pantalla vacía.
  static const AppMessage daemonDown = AppMessage(
    severity: MessageSeverity.warn,
    title: 'No hay conexión con el servicio de Kanpachi',
    body:
        'La app no pudo hablar con el servicio que hace el trabajo, así que '
        'no puede abrir salas ni leer el catálogo. Comprueba que Kanpachi '
        'esté instalado y que su servicio esté arrancado.',
  );

  /// El registro no contesta, así que no se puede abrir ni entrar a ninguna
  /// sala.
  ///
  /// **No es [codeLost] y la diferencia manda el texto.** Allá el registro
  /// contestó que no conoce un código, eso no se arregla nunca y hay un botón
  /// que lo cierra. Acá no contestó nada, se arregla solo en cuanto vuelva, y
  /// no hay nada que pulsar: por eso este mensaje no lleva acción y lo dice.
  ///
  /// Existe desde que crear y entrar fallan rápido sin registro. Antes esto se
  /// descubría al final de una espera, después de elegir un juego y escribir
  /// ocho caracteres; el aviso lo pone delante, con la pantalla todavía vacía.
  static const AppMessage seedDown = AppMessage(
    severity: MessageSeverity.warn,
    title: 'El servidor de encuentro no responde',
    body:
        'Es la máquina por la que las salas se encuentran, así que ahora mismo '
        'no se puede crear ninguna ni entrar a una. No hay nada que hacer de '
        'tu lado: en cuanto vuelva, esto se va solo.',
  );

  /// El de reserva, para una clave que esta versión de la app no conoce.
  ///
  /// Existe porque el daemon y la UI se actualizan por separado, así que puede
  /// llegar una clave más nueva que esta app. Decir "pasó algo que no sé
  /// explicar" es honesto; quedarse en blanco no lo es, y romper la pantalla
  /// menos.
  static const AppMessage unknown = AppMessage(
    severity: MessageSeverity.warn,
    body:
        'Kanpachi reportó algo que esta versión de la app no sabe explicar. '
        'Copia el diagnóstico y avisa en el grupo.',
  );

  /// Lo mismo que [alert], para lo que llega crudo del cable.
  ///
  /// Es el punto de entrada honesto: una clave desconocida no rompe la pantalla
  /// ni desaparece, sale como [unknown] llevándose el detalle del daemon, que
  /// es lo único que en ese caso sigue siendo útil.
  static AppMessage alertFromWire(String? wire, {String? detail}) {
    final AlertKind? kind = AlertKind.fromWire(wire);
    if (kind == null) return unknown.withDetail(detail);
    return alert(kind, detail: detail);
  }

  static AppMessage exitFromWire(String? wire) {
    final ExitReason? reason = ExitReason.fromWire(wire);
    // Sin motivo no hay nada que decir: es lo que pasa cuando la app arranca
    // por primera vez y nunca hubo sesión anterior.
    if (reason == null) return AppMessage.none;
    return exit(reason);
  }

  static AppMessage failureFromWire(String? wire) {
    final FailureCode? code = FailureCode.fromWire(wire);
    if (code == null) return unknown;
    return failure(code);
  }

  /// An action the user asked for, that did not happen.
  ///
  /// # Why the ACTION and not just the code
  ///
  /// Because the code says what went wrong and the action says what did not
  /// happen, and the person reading only cares about the second. `unavailable`
  /// is the same code whether the room could not be created or the invite code
  /// could not be renewed, and those are two very different things to be told.
  ///
  /// So the title comes from the action and the explanation from the code. When
  /// the daemon never answered there is no code, and the sentence is another
  /// one entirely: nothing was even asked, so nothing half happened.
  static AppMessage actionFailed(FailedAction action, {String? code}) {
    final FailureCode? resolved = FailureCode.fromWire(code);
    final AppMessage base = code == null
        ? _sinServicio
        : (resolved == null ? unknown : failure(resolved));
    return AppMessage(
      severity: MessageSeverity.error,
      title: _actionTitle(action),
      body: base.body,
      hint: base.hint,
    );
  }

  /// What did not happen. One sentence per button that can fail.
  ///
  /// A single generic "algo salió mal" is the message that teaches people to
  /// stop reading messages.
  static String _actionTitle(FailedAction action) => switch (action) {
    FailedAction.createRoom => 'No se pudo abrir la sala',
    FailedAction.joinRoom => 'No se pudo entrar a la sala',
    FailedAction.leaveRoom => 'No se pudo salir de la sala',
    FailedAction.activateProfile => 'No se pudo cambiar el juego',
    FailedAction.kickMember => 'No se pudo expulsar',
    FailedAction.rotateInviteCode => 'No se pudo renovar el código',
    FailedAction.renameRoom => 'No se pudo cambiar el nombre',
    FailedAction.resumeRoom => 'No se pudo reabrir la sala anterior',
    FailedAction.discardSavedRoom => 'No se pudo descartar la sala anterior',
    FailedAction.reapplyProtection => 'No se pudo reponer la protección',
    FailedAction.probeHost => 'No se pudo comprobar los puertos del host',
    FailedAction.loadCatalog => 'No se pudo leer el catálogo de juegos',
    FailedAction.saveProfile => 'No se pudo guardar el perfil',
    FailedAction.importCatalog => 'No se pudo importar el catálogo',
    FailedAction.exportCatalog => 'No se pudo exportar el catálogo',
    FailedAction.suspendForeignRules => 'No se pudo cambiar la regla del juego',
    FailedAction.quit => 'No se pudo cerrar Kanpachi',
  };

  /// The daemon never answered.
  ///
  /// Kept apart from every [FailureCode] on purpose: those are the daemon
  /// saying no, and this is the daemon not being there. Nothing was asked, so
  /// nothing half happened, and that is the one thing worth saying — it makes
  /// retrying safe.
  static const AppMessage _sinServicio = AppMessage(
    severity: MessageSeverity.error,
    body:
        'El servicio de Kanpachi no contestó, así que no llegó a intentarse. '
        'Vuelve a probar.',
    hint: 'Si sigue igual, cierra Kanpachi y ábrelo otra vez.',
  );
}
