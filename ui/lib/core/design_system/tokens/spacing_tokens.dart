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

  /// Con la que abre la PRIMERA vez, y solo la primera.
  ///
  /// A partir de ahí manda lo que el usuario haya dejado: el tamaño se recuerda
  /// entre arranques, ver [AppPreferences.windowSize]. Esto es el punto de
  /// partida, no un tope ni una preferencia.
  ///
  /// **Es más angosta que [contentMax], y a propósito.** El ancho de contenido
  /// es un TOPE, no una medida a la que haya que llegar: por debajo de él las
  /// pantallas se acomodan solas, que es lo que hacen igualmente en cualquier
  /// ventana que no esté maximizada. Abrir a 940 y pico llenaba media pantalla
  /// de un portátil para enseñar una portada con cuatro botones.
  ///
  /// El alto no sale del archivo de diseño: sale de que una ventana que abre
  /// ocupando media pantalla es una ventana que hay que encoger, y encogerla
  /// es lo que nadie hace. Cabe la portada entera, y las pantallas largas
  /// hacen scroll, que es lo que hacen igual en cualquier tamaño.
  ///
  /// El ancho es [contentMax] y no un número aparte: por debajo de ese tope las
  /// pantallas se acomodan solas, así que abrir justo en él es lo único que
  /// enseña el diseño sin margen sobrante a los lados ni renglones más largos
  /// de lo que nadie dibujó.
  static const Size initialWindow = Size(940, 625);

  static const double titleBarHeight = 44;
  static const double statusBarHeight = 38;
}

/// Radios de esquina.
abstract final class AppRadius {
  static const Radius xs = Radius.circular(6);
  static const Radius s7 = Radius.circular(7);
  static const Radius sm = Radius.circular(8);
  static const Radius s10 = Radius.circular(10);
  static const Radius md = Radius.circular(12);
  static const Radius lg = Radius.circular(14);
  static const Radius xl = Radius.circular(16);
  static const Radius xxl = Radius.circular(20);

  static const BorderRadius allXs = BorderRadius.all(xs);

  /// Los escalones intermedios existen porque el diseño escala el radio con el
  /// tamaño del hueco: una miniatura de 46 px no lleva el mismo redondeo que
  /// una portada de 150.
  static const BorderRadius all7 = BorderRadius.all(s7);
  static const BorderRadius allSm = BorderRadius.all(sm);
  static const BorderRadius all10 = BorderRadius.all(s10);
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
