import 'package:flutter/widgets.dart';

/// Espaciados, radios y grosores.
///
/// `static const` a propósito, y no un `ThemeExtension`. La razón es la misma
/// que prohíbe los métodos que devuelven Widget: una extensión se lee por
/// `context`, así que nunca es const, y cualquier widget que leyera su padding
/// por context perdería su constructor `const` — y con él la rama de salto
/// total de `Element.updateChild`, la que ni siquiera reconstruye. El color
/// paga ese precio porque cambia en runtime y anima; un padding no hace
/// ninguna de las dos cosas, así que no tiene por qué pagarlo.
abstract final class AppSpacing {
  static const double xxs = 2;
  static const double xs = 4;
  static const double sm = 6;
  static const double md = 8;
  static const double lg = 10;
  static const double xl = 12;
  static const double xxl = 14;
  static const double x3l = 16;
  static const double x4l = 18;
  static const double x5l = 20;
  static const double x6l = 22;
  static const double x7l = 24;
  static const double x8l = 26;
  static const double x9l = 30;
  static const double x10l = 40;

  /// El margen horizontal del contenido dentro de la tarjeta. No depende de la
  /// densidad: lo que se comprime es el aire vertical, no el ancho de lectura.
  static const double pageInline = 30;

  /// Hasta dónde crece el contenido antes de dejar de crecer y centrarse.
  ///
  /// Es el ancho de la ventana que dibuja el diseño, y arriba de eso no hay
  /// diseño: una ventana maximizada en un monitor de 1440p daría dos columnas
  /// de 1200 px y renglones que no se pueden leer de corrido. Que sobre margen
  /// a los lados es la respuesta correcta, no un desperdicio.
  static const double contentMax = 940;

  /// El tamaño mínimo de la ventana, el que impone `main()`.
  ///
  /// Vive acá y no suelto en `main.dart` porque es la condición de contorno de
  /// todo layout de la app: por debajo de esto nadie tiene que responder, y por
  /// encima todo tiene que aguantar. El test de layout prueba exactamente este
  /// tamaño, así que subirlo sin mirar deja pantallas sin cubrir.
  static const Size minWindow = Size(720, 520);

  /// Con la que abre. El ancho del diseño más el aire de los lados; el alto da
  /// para la sala entera sin scroll, que es la pantalla más larga de uso
  /// normal.
  static const Size initialWindow = Size(1000, 720);

  static const double titleBarHeight = 44;
  static const double statusBarHeight = 38;
}

/// Radios de esquina.
abstract final class AppRadius {
  static const Radius xs = Radius.circular(6);
  static const Radius sm = Radius.circular(8);
  static const Radius md = Radius.circular(12);
  static const Radius lg = Radius.circular(14);
  static const Radius xl = Radius.circular(16);
  static const Radius xxl = Radius.circular(20);

  static const BorderRadius allXs = BorderRadius.all(xs);
  static const BorderRadius allSm = BorderRadius.all(sm);
  static const BorderRadius allMd = BorderRadius.all(md);
  static const BorderRadius allLg = BorderRadius.all(lg);
  static const BorderRadius allXl = BorderRadius.all(xl);
  static const BorderRadius allXxl = BorderRadius.all(xxl);

  /// Para las píldoras. 999 en CSS; acá basta con un número mayor que
  /// cualquier media altura que vayamos a usar.
  static const BorderRadius pill = BorderRadius.all(Radius.circular(999));
}

/// Grosores de borde.
abstract final class AppStroke {
  static const double hairline = 1;
  static const double thick = 1.5;
}
