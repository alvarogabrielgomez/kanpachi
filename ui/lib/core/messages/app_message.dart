import 'package:flutter/foundation.dart';

/// Cuánto pesa un mensaje, y de ahí sale cómo se pinta.
///
/// Son tres y no cinco a propósito. Cada nivel más es una decisión que alguien
/// tiene que tomar en cada call site, y la práctica dice que con cinco nadie
/// distingue el tercero del cuarto.
///
/// **Ninguno es rojo.** La paleta del producto no tiene un tono de urgencia, y
/// es deliberado: lo que estos mensajes describen es mejorable, no roto. Ver
/// `AppNoticeTone` en el design system.
enum MessageSeverity {
  /// Ámbar. Algo que el usuario puede arreglar y le conviene arreglar.
  warn,

  /// Neutro. Un hecho que cambia lo que se puede hacer y no es culpa de nadie.
  neutral,

  /// Confirmación de algo que ya pasó.
  done,
}

/// Un mensaje del producto, ya resuelto y listo para pintar.
///
/// # Por qué son datos y no un Widget
///
/// El catálogo devuelve esto y no un `Widget`, y la diferencia no es de estilo:
///
///  - Se puede afirmar en un test sin montar un `MaterialApp` ni bombear
///    frames. Los tests que cubren el catálogo entero se leen como una tabla.
///  - No arrastra un `BuildContext`, así que el catálogo se puede consultar
///    desde un cubit, desde el título de una ventana o desde el icono de la
///    bandeja, que son tres sitios donde no hay dónde pintar.
///  - Mantiene la regla de la casa sin esfuerzo: quien pinta es una clase
///    widget, nunca una función que devuelve `Widget`.
///
/// # La forma del texto, y por qué es esta
///
/// La regla de microcopy del producto, textual de `docs/05-ui.md`: *"cada error
/// dice qué pasó, qué significa para el usuario y qué hacer, en ese orden, en
/// español coloquial. Jamás un código de error a secas"*.
///
/// Los campos son esa regla convertida en tipo. [title] es qué pasó, [body] es
/// qué significa y qué hacer, y [hint] es el tramo final que señala dónde
/// mirar. Un mensaje sin [body] no se puede construir, así que el aviso que
/// deja al usuario con el susto y sin salida no se puede escribir por descuido.
@immutable
class AppMessage {
  const AppMessage({
    required this.severity,
    required this.body,
    this.title,
    this.hint,
    this.detail,
  });

  /// El mensaje vacío: hay situaciones cuyo texto correcto es ninguno.
  ///
  /// Salir de la sala por tu cuenta es la principal. `docs/05-ui.md` lo dice
  /// así: *"Sin texto. No hay nada que explicar"*. Existe como valor y no como
  /// `null` para que el catálogo pueda ser total, o sea que toda clave tenga
  /// respuesta, y quien pregunte no tenga que distinguir "no sé" de "no hay
  /// nada que decir".
  static const AppMessage none =
      AppMessage(severity: MessageSeverity.neutral, body: '');

  /// Qué pasó. Opcional: los avisos de una sola línea no llevan.
  final String? title;

  /// Qué significa para el usuario y qué hacer. Obligatorio.
  final String body;

  /// El tramo final que señala dónde mirar, tipo "Cómo activarlo".
  ///
  /// Va aparte del cuerpo porque se pinta en color de acento, y va como texto y
  /// no como acción con callback porque hoy no lleva a ningún sitio: la página
  /// de ayuda no existe. Un enlace que se puede pulsar y no hace nada es peor
  /// que uno que solo señala.
  final String? hint;

  /// El dato concreto que aportó el daemon, si lo hubo.
  ///
  /// Es el campo `detail` del cable, tipo "el router publica el puerto 25565
  /// hacia 192.168.1.7". **No sustituye al cuerpo**: el copy lo decide la clave
  /// y esto lo acompaña. Así el texto del producto se escribe una vez acá y no
  /// depende de cómo redacte el daemon.
  final String? detail;

  /// Si no hay nada que mostrar. Ver [none].
  bool get isEmpty => body.isEmpty && title == null;

  AppMessage withDetail(String? value) {
    if (value == null || value.isEmpty) return this;
    return AppMessage(
      severity: severity,
      title: title,
      body: body,
      hint: hint,
      detail: value,
    );
  }

  final MessageSeverity severity;

  @override
  bool operator ==(Object other) =>
      other is AppMessage &&
      other.severity == severity &&
      other.title == title &&
      other.body == body &&
      other.hint == hint &&
      other.detail == detail;

  @override
  int get hashCode => Object.hash(severity, title, body, hint, detail);

  @override
  String toString() => 'AppMessage(${severity.name}, ${title ?? '-'}: $body)';
}
