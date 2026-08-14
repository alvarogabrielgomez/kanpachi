import 'package:flutter/foundation.dart';

/// Un enlace `kanpachi://` que llegó de fuera y todavía nadie confirmó.
///
/// Es una invariante del proyecto hecha dato: **nada que llegue de fuera surte
/// efecto sin confirmación dentro**. El enlace abre la app y produce esto; lo
/// que hace entrar a la sala es que alguien pulse el botón.
///
/// Todo lo que no sea [link] puede venir vacío, y ninguna ausencia impide
/// entrar. La tarjeta es presentación, y el registro puede estar caído: entrar
/// a una sala no pasa por él.
@immutable
class PendingInvite {
  const PendingInvite({
    required this.link,
    this.code = '',
    this.seed = '',
    this.roomName = '',
    this.hostNick = '',
    this.unknown = false,
    this.fingerprint = '',
    this.verdict = HostVerdict.unverified,
    this.knownNick = '',
    this.knownFingerprint = '',
    this.knownRooms = 0,
  });

  factory PendingInvite.fromJson(Map<String, Object?> json) => PendingInvite(
    link: json['link'] as String? ?? '',
    code: json['code'] as String? ?? '',
    seed: json['seed'] as String? ?? '',
    roomName: json['room'] as String? ?? '',
    hostNick: json['host'] as String? ?? '',
    unknown: json['unknown'] as bool? ?? false,
    fingerprint: json['fingerprint'] as String? ?? '',
    verdict: HostVerdict.fromWire(json['verdict'] as String? ?? ''),
    knownNick: json['known_nick'] as String? ?? '',
    knownFingerprint: json['known_fingerprint'] as String? ?? '',
    knownRooms: json['known_rooms'] as int? ?? 0,
  );

  /// Lo que llegó, tal cual, sin interpretar.
  ///
  /// Se guarda aunque no se haya entendido, y ese es su motivo: alguien pulsó
  /// un botón en su navegador, y si no se entendió hay que poder enseñarle qué
  /// llegó. Es texto de fuera y la pantalla lo trata como tal.
  final String link;

  /// El invite ID legible. Vacío significa que el enlace NO se entendió.
  final String code;

  final String seed;

  /// El nombre de la sala, de la tarjeta. Vacío cuando no se pudo abrir.
  final String roomName;

  /// Cómo se identifica quien abrió la sala. **No está verificado**, así que la
  /// pantalla dice «se identifica como» y jamás «te invitó».
  final String hostNick;

  /// El registro AFIRMÓ que esa sala no existe.
  ///
  /// Distinto de que no contestara: aquello deja esto en false, porque la
  /// ausencia de información no es una respuesta y la pantalla dice otra frase.
  final bool unknown;

  /// La huella de la llave que firmó la tarjeta de esta sala.
  ///
  /// Vacía cuando no se verificó nada, y entonces la pantalla no dice nada de
  /// identidad: una huella que no respalda a nadie es un número que decora, y
  /// enseñarla enseña a ignorarla donde sí significa algo.
  final String fingerprint;

  /// Qué dice la libreta de esa huella. Ver [HostVerdict].
  final HostVerdict verdict;

  /// Lo que la libreta recuerda: con qué nombre, con qué huella y en cuántas
  /// salas. Es lo que convierte el aviso en algo que se puede juzgar, porque
  /// pone la huella de antes al lado de la que acaba de llegar.
  final String knownNick;
  final String knownFingerprint;
  final int knownRooms;

  /// Si hay una sala a la que ofrecerse entrar.
  bool get understood => code.isNotEmpty;

  /// Si hay algo que decir sobre quién hospeda.
  bool get hasHostTrust =>
      fingerprint.isNotEmpty && verdict != HostVerdict.unverified;
}

/// Qué dice la libreta de huellas del host de una sala.
///
/// La libreta vive en el daemon; esto es solo cómo se pinta. Ninguno de los
/// cuatro estados impide entrar: el aviso avisa. Signal empezó bloqueando ante
/// un cambio de llave y se movió a avisar, porque la gente reinstala, y un
/// bloqueo dentro de un juego es un botón que se pulsa sin leer.
enum HostVerdict {
  /// No había con qué juzgar: nadie firmó, o la firma no verificó.
  unverified,

  /// Una llave que esta máquina no había visto. Es lo normal de toda primera
  /// invitación, y no es una alarma.
  nueva,

  /// La misma llave y el mismo nombre que la última vez.
  conocida,

  /// La misma llave con otro nombre. Alguien se cambió el apodo, que se puede.
  renombrada,

  /// Un nombre conocido llegando con OTRA llave. Es el aviso.
  llaveCambiada;

  static HostVerdict fromWire(String s) => switch (s) {
    'new' => HostVerdict.nueva,
    'known' => HostVerdict.conocida,
    'renamed' => HostVerdict.renombrada,
    'key-changed' => HostVerdict.llaveCambiada,
    // Lo que no se reconoce no se pinta. Una versión del daemon más nueva que
    // esta ventana manda una palabra que acá no existe, y dibujar el aviso
    // equivocado con seguridad es peor que no dibujar ninguno.
    _ => HostVerdict.unverified,
  };
}
