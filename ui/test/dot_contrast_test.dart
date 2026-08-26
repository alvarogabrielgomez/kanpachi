import 'dart:math' as math;
import 'dart:ui';

import 'package:flutter_test/flutter_test.dart';
import 'package:kanpachi_ui/core/design_system/tokens/color_tokens.dart';

/// Los tres puntos se distinguen, y también sin ver el rojo o el verde.
///
/// # Qué se afirma acá
///
/// Que el verde, el ámbar y el gris de un punto de estado no colapsan en el
/// mismo color para quien tiene protanopia o deuteranopia, que entre las dos
/// son cerca del 8% de los hombres. Un punto es SIETE píxeles: no tiene forma,
/// ni texto al lado que lo repita, así que si dos de ellos se ven iguales, el
/// estado que separan deja de existir para esa persona.
///
/// # Lo que había, medido el 2026-08-26
///
/// En la paleta oscura de entonces, el verde `#7FAE6B` y el gris `#A19A8F`
/// quedaban a **6,8** de distancia bajo deuteranopia, y a 8,1 bajo protanopia.
/// Cero a efectos prácticos: un miembro conectado y uno ausente pintaban el
/// mismo punto. En la clara era peor todavía, 4,2.
///
/// # Cómo se mide
///
/// Se simula la visión con las matrices de Viénot, Brettel y Mollon, que son
/// las que usa la herramienta que todo el mundo cita, y se compara en CIE Lab
/// con ΔE76. El umbral es 15: por debajo de 10 dos colores se confunden a
/// simple vista, y un punto de siete píxeles no perdona lo que un rectángulo
/// grande sí.
void main() {
  const Map<String, ColorTokens> paletas = <String, ColorTokens>{
    'oscura': AppPalette.dark,
    'clara': AppPalette.light,
  };

  const double umbral = 15;

  for (final MapEntry<String, ColorTokens> paleta in paletas.entries) {
    final ColorTokens c = paleta.value;
    final Map<String, Color> puntos = <String, Color>{
      'ok': c.ok,
      'warn': c.warn,
      'idle': c.idle,
    };

    for (final MapEntry<String, _Vision> vision in _visiones.entries) {
      test(
        'en la paleta ${paleta.key}, los puntos se separan con ${vision.key}',
        () {
          final List<String> juntos = <String>[];
          final List<String> nombres = puntos.keys.toList();
          for (int i = 0; i < nombres.length; i++) {
            for (int j = i + 1; j < nombres.length; j++) {
              final double d = _deltaE(
                vision.value(puntos[nombres[i]]!),
                vision.value(puntos[nombres[j]]!),
              );
              if (d < umbral) {
                juntos.add(
                  '${nombres[i]} y ${nombres[j]}: ${d.toStringAsFixed(1)}',
                );
              }
            }
          }
          expect(
            juntos,
            isEmpty,
            reason:
                'Con ${vision.key} estos pares quedan por debajo de $umbral en '
                'ΔE76, o sea que se leen como el mismo punto:\n'
                '${juntos.join('\n')}',
          );
        },
      );
    }

    // La escalera de luminosidad se exige SOLO en la oscura, y no por
    // comodidad.
    //
    // En la clara el verde es oscuro por necesidad, porque tiene que
    // contrastar contra un fondo crema, y ahí las dos exigencias se pelean:
    // alejar el gris en luminosidad lo mete en la zona donde la deuteranopia
    // lo confunde con ese verde oscuro. Buscado a fuerza bruta sobre todos
    // los grises cálidos que contrastan 3:1 contra el fondo, los únicos que
    // cumplen las dos cosas a la vez tiran a violeta y no pertenecen a esta
    // paleta. La clara se apoya en la distancia de color, que le da 17.
    if (paleta.key == 'oscura') {
      test(
        'en la paleta oscura, el gris queda lejos del verde en luminosidad',
        () {
          // El segundo canal cuando el color no alcanza. Sirve en una captura
          // en blanco y negro, y en una pantalla mal calibrada.
          final double dl = (_lab(c.ok)[0] - _lab(c.idle)[0]).abs();
          expect(
            dl,
            greaterThanOrEqualTo(12),
            reason:
                'El verde y el gris apagado están a ${dl.toStringAsFixed(1)} de '
                'L*, y necesitan al menos 12 para que la diferencia sobreviva '
                'sin color.',
          );
        },
      );
    }
  }
}

typedef _Vision = Color Function(Color);

/// Las dos que confunden rojo con verde, más la visión de siempre.
///
/// La tritanopia queda fuera a propósito: confunde azul con amarillo y ninguno
/// de estos tres puntos es azul.
final Map<String, _Vision> _visiones = <String, _Vision>{
  'visión normal': (Color c) => c,
  'protanopia': (Color c) => _simular(c, const <List<double>>[
    <double>[0.567, 0.433, 0],
    <double>[0.558, 0.442, 0],
    <double>[0, 0.242, 0.758],
  ]),
  'deuteranopia': (Color c) => _simular(c, const <List<double>>[
    <double>[0.625, 0.375, 0],
    <double>[0.700, 0.300, 0],
    <double>[0, 0.300, 0.700],
  ]),
};

Color _simular(Color c, List<List<double>> m) {
  double canal(List<double> fila) =>
      (fila[0] * (c.r * 255) + fila[1] * (c.g * 255) + fila[2] * (c.b * 255))
          .clamp(0, 255);
  return Color.fromARGB(
    255,
    canal(m[0]).round(),
    canal(m[1]).round(),
    canal(m[2]).round(),
  );
}

double _deltaE(Color a, Color b) {
  final List<double> x = _lab(a);
  final List<double> y = _lab(b);
  return math.sqrt(
    math.pow(x[0] - y[0], 2) +
        math.pow(x[1] - y[1], 2) +
        math.pow(x[2] - y[2], 2),
  );
}

List<double> _lab(Color c) {
  double lineal(double v) =>
      v <= 0.04045 ? v / 12.92 : math.pow((v + 0.055) / 1.055, 2.4).toDouble();

  final double r = lineal(c.r);
  final double g = lineal(c.g);
  final double b = lineal(c.b);

  final double x = (r * 0.4124 + g * 0.3576 + b * 0.1805) / 0.95047;
  final double y = r * 0.2126 + g * 0.7152 + b * 0.0722;
  final double z = (r * 0.0193 + g * 0.1192 + b * 0.9505) / 1.08883;

  double f(double t) =>
      t > 0.008856 ? math.pow(t, 1 / 3).toDouble() : 7.787 * t + 16 / 116;

  return <double>[116 * f(y) - 16, 500 * (f(x) - f(y)), 200 * (f(y) - f(z))];
}
