import 'package:flutter/animation.dart';

/// Duraciones y curvas. `static const` por el mismo motivo que el spacing: no
/// cambian en runtime, así que no tienen por qué costarle el `const` a nadie.
abstract final class AppMotion {
  /// El rebote del botón principal al presionarlo.
  static const Duration press = Duration(milliseconds: 140);

  /// Cambios de color y borde al pasar por encima.
  static const Duration hover = Duration(milliseconds: 160);

  /// La entrada de un diálogo.
  static const Duration dialog = Duration(milliseconds: 200);

  /// La entrada de una pantalla.
  static const Duration screen = Duration(milliseconds: 300);

  /// Una vuelta del indicador de carga.
  static const Duration spin = Duration(seconds: 1);

  /// El latido del punto de "servicio activo".
  static const Duration pulseSlow = Duration(milliseconds: 2600);

  /// El latido del punto de un aviso de red, más nervioso a propósito.
  static const Duration pulseFast = Duration(milliseconds: 1600);

  /// Cuánto tarda el fondo ambiental en dar una vuelta completa. Son cuatro
  /// manchas con periodos distintos y primos entre sí para que el conjunto no
  /// repita un patrón reconocible.
  static const List<Duration> drift = <Duration>[
    Duration(seconds: 17),
    Duration(seconds: 21),
    Duration(seconds: 19),
    Duration(seconds: 24),
  ];

  /// Lo mismo para las SEIS manchas del fondo a pantalla completa. Periodos
  /// propios y no un recorte de [drift]: con seis manchas repartidas por toda
  /// la ventana, dos que compartan periodo se ven latir juntas.
  static const List<Duration> driftScreen = <Duration>[
    Duration(seconds: 19),
    Duration(seconds: 25),
    Duration(seconds: 23),
    Duration(seconds: 21),
    Duration(seconds: 17),
    Duration(seconds: 29),
  ];

  /// Cuánto se queda un botón diciendo "Copiado" antes de volver a su texto.
  static const Duration copiedFeedback = Duration(milliseconds: 1700);

  /// Lo MÍNIMO que una frase de la pantalla de carga se queda en pantalla.
  ///
  /// Es un freno, no un ritmo: las frases avanzan con los pasos reales del
  /// daemon, y esos pueden llegar de golpe — entrar a una sala de la misma LAN
  /// resuelve media operación en un suspiro. Sin este piso, media docena de
  /// frases pasarían en un parpadeo y ninguna se leería.
  static const Duration phraseDwell = Duration(milliseconds: 900);

  /// La entrada de una frase nueva, que sube unos píxeles mientras aparece.
  static const Duration phraseFade = Duration(milliseconds: 420);

  /// Lo que tarda la barra de progreso en llegar a su ancho nuevo.
  static const Duration barFill = Duration(milliseconds: 500);

  /// El brillo que recorre la barra de progreso, de punta a punta.
  static const Duration sheen = Duration(milliseconds: 1500);

  /// El latido de los puntos suspensivos que siguen a la frase.
  static const Duration dots = Duration(milliseconds: 1200);

  static const Curve enter = Curves.easeOut;
  static const Curve standard = Curves.easeInOut;

  /// La curva del relleno de la barra: sale rápido y frena al final, para que
  /// un salto de tres pasos se lea como un avance y no como un tirón.
  static const Curve fill = Cubic(0.22, 0.7, 0.2, 1);
}
