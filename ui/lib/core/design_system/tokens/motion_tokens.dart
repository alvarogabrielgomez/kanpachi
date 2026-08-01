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

  /// Cuánto se queda un botón diciendo "Copiado" antes de volver a su texto.
  static const Duration copiedFeedback = Duration(milliseconds: 1700);

  static const Curve enter = Curves.easeOut;
  static const Curve standard = Curves.easeInOut;
}
