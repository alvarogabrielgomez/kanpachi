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
            body: 'Kanpachi se apoya en él para que nadie de la sala alcance '
                'tu PC. Sin él, no puede protegerte. ',
            hint: 'Cómo activarlo',
          ),
        AlertKind.rulesTampered => const AppMessage(
            severity: MessageSeverity.warn,
            title: 'Algo está borrando las reglas de Kanpachi',
            body: 'Suele ser un antivirus. Mientras pase, la sala puede no '
                'funcionar.',
          ),
        AlertKind.routerMapping => const AppMessage(
            severity: MessageSeverity.warn,
            title: 'Tu router tiene un puerto abierto hacia internet',
            body: 'Kanpachi no lo necesita: hace el túnel sin abrir nada. '
                'Mientras siga así, cualquiera en internet llega a ese '
                'puerto. ',
            hint: 'Cómo cerrarlo',
          ),
        AlertKind.foreignRule => const AppMessage(
            severity: MessageSeverity.warn,
            title: 'El juego dejó una regla que lo hace visible en tu red',
            body: 'Quien esté en tu red de casa llega al juego sin pasar por '
                'Kanpachi, y seguirá llegando aunque saques a alguien de la '
                'sala. Se puede desactivar mientras dure la sala y se '
                'restaura sola al salir.',
          ),
        AlertKind.lobbyConflict => const AppMessage(
            severity: MessageSeverity.warn,
            title: 'Tu red usa el mismo rango que el vestíbulo de Kanpachi',
            body: 'Entrar a salas de otros puede fallar. Es el único rango que '
                'Kanpachi no puede mover, porque tiene que ser el mismo en '
                'todas las máquinas.',
          ),
        AlertKind.kickIncomplete => const AppMessage(
            severity: MessageSeverity.warn,
            title: 'La expulsión quedó a medias',
            body: 'Ya no está en la lista de la sala y su máquina puede seguir '
                'autorizada. Renueva el código para cerrarle la puerta del '
                'todo.',
          ),

        // La que no se parece a ninguna: las demás cuentan un hallazgo, esta
        // cuenta que no hubo con qué mirar. Sin decirlo, una auditoría caída y
        // una máquina impecable se ven idénticas, porque las dos pintan verde.
        AlertKind.auditFailed => const AppMessage(
            severity: MessageSeverity.warn,
            title: 'Kanpachi no pudo comprobar cómo está tu PC',
            body: 'No quiere decir que algo esté mal: quiere decir que nadie '
                'lo está vigilando. Reinicia Kanpachi y si sigue igual, copia '
                'el diagnóstico.',
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
  static AppMessage foreignRule(RuleClass kind, {String? gameName, String? detail}) =>
      _foreignRule(kind, gameName).withDetail(detail);

  static AppMessage _foreignRule(RuleClass kind, String? gameName) =>
      switch (kind) {
        RuleClass.game => AppMessage(
            severity: MessageSeverity.warn,
            title: '${gameName ?? 'El juego'} dejó una regla en tu firewall',
            body: 'Con ella el juego es alcanzable desde tu red de casa y por '
                'toda la sala, sin pasar por el control de Kanpachi: expulsar '
                'a alguien no lo tapa. Se puede desactivar mientras juegas y '
                'se devuelve al salir.',
          ),

        // La única que impide abrir la sala, y por eso no ofrece "dejar así"
        // como una opción equivalente a la otra. El texto nombra lo que se
        // entrega en vez de decir "riesgo de seguridad", porque lo primero se
        // entiende y lo segundo se despacha.
        RuleClass.remoteControl => const AppMessage(
            severity: MessageSeverity.warn,
            title: 'Un programa de control remoto está abierto en tu firewall',
            body: 'Quien entre a la sala puede llegar a él y quedarse con tu '
                'teclado, tu pantalla y tus archivos, aunque lo expulses '
                'después: el código de invitación no es un secreto. Hay que '
                'resolverlo antes de abrir la sala. Se puede desactivar '
                'mientras juegas y se devuelve al salir.',
          ),

        // Se muestra igual en vez de callarse. Kanpachi la encontró buscando lo
        // que abre la máquina, así que decir "hay algo que no sé clasificar" es
        // más honesto que no decir nada.
        RuleClass.other => const AppMessage(
            severity: MessageSeverity.neutral,
            title: 'Hay una regla en tu firewall que Kanpachi no puso',
            body: 'No es del juego ni de un programa de control remoto '
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
            body: 'El host llevaba veinte minutos sin aparecer, así que '
                'Kanpachi cerró todo lo que había abierto.',
          ),
        ExitReason.tunnelLost => const AppMessage(
            severity: MessageSeverity.neutral,
            body: 'La conexión con la sala no volvió. Kanpachi lo intentó '
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
            body: 'Ese código no parece completo. Son 8 caracteres, revisa que '
                'se copió entero.',
          ),
        FailureCode.badNickname => const AppMessage(
            severity: MessageSeverity.warn,
            body: 'Ese nombre no se puede usar. Van entre 2 y 20 caracteres, '
                'sin símbolos raros.',
          ),
        FailureCode.unavailable => const AppMessage(
            severity: MessageSeverity.warn,
            body: 'No hay conexión con el servidor de encuentro. Revisa tu '
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
            body: 'Ese juego no está en el catálogo. Elígelo de la biblioteca '
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
            body: 'Ya hay un perfil con ese nombre entre los que vinieron con '
                'la app. Ponle otro para no taparlo.',
          ),
        FailureCode.notPlayed => const AppMessage(
            severity: MessageSeverity.warn,
            body: 'Un perfil se marca verificado después de jugarlo con '
                'alguien más, no antes.',
          ),
        FailureCode.badProfile => const AppMessage(
            severity: MessageSeverity.warn,
            body: 'Ese perfil pide algo que Kanpachi no abre nunca. Lo peor '
                'que puede pasar es que ese juego no conecte.',
          ),
        FailureCode.kickPartial => const AppMessage(
            severity: MessageSeverity.warn,
            body: 'La expulsión quedó a medias. Renueva el código para '
                'cerrarle la puerta del todo.',
          ),
        FailureCode.noPending => const AppMessage(
            severity: MessageSeverity.neutral,
            body: 'No quedó ninguna sala a medio cerrar del arranque anterior.',
          ),

        // Los cuatro que el usuario no puede accionar. Ver el doc de arriba.
        FailureCode.badRequest ||
        FailureCode.tooLarge ||
        FailureCode.unauthorized ||
        FailureCode.internal =>
          const AppMessage(
            severity: MessageSeverity.warn,
            body: 'Algo falló dentro de Kanpachi. Reinicia la app, y si sigue '
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
        ConnState.connected =>
          AppMessage.none,
        ConnState.degraded => const AppMessage(
            severity: MessageSeverity.warn,
            body: 'Conexión inestable, reintentando',
          ),
        ConnState.reconnecting => const AppMessage(
            severity: MessageSeverity.warn,
            body: 'Reconectando…',
          ),
      };

  /// El host se fue y el juego corría en su PC.
  ///
  /// Lleva el nombre dentro, así que es de los pocos que reciben un dato. El
  /// respaldo no es un hueco vacío: sin nombre, "El host" dice lo mismo.
  static AppMessage hostLeft(String? hostName) => AppMessage(
        severity: MessageSeverity.neutral,
        title: '${hostName ?? 'El host'} salió de la sala',
        body: 'El juego corría en su PC, así que no hay a qué conectarse '
            'hasta que vuelva. La sala sigue en pie.',
      );

  /// El de reserva, para una clave que esta versión de la app no conoce.
  ///
  /// Existe porque el daemon y la UI se actualizan por separado, así que puede
  /// llegar una clave más nueva que esta app. Decir "pasó algo que no sé
  /// explicar" es honesto; quedarse en blanco no lo es, y romper la pantalla
  /// menos.
  static const AppMessage unknown = AppMessage(
    severity: MessageSeverity.warn,
    body: 'Kanpachi reportó algo que esta versión de la app no sabe explicar. '
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
}
