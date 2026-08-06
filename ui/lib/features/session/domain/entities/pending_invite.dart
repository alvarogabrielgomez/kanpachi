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
  });

  factory PendingInvite.fromJson(Map<String, Object?> json) => PendingInvite(
    link: json['link'] as String? ?? '',
    code: json['code'] as String? ?? '',
    seed: json['seed'] as String? ?? '',
    roomName: json['room'] as String? ?? '',
    hostNick: json['host'] as String? ?? '',
    unknown: json['unknown'] as bool? ?? false,
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

  /// Si hay una sala a la que ofrecerse entrar.
  bool get understood => code.isNotEmpty;
}
