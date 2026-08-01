import 'dart:math';

/// Los nombres que se sugieren al crear una sala.
///
/// Que haya sugerencia y no un campo vacío importa más de lo que parece:
/// nadie quiere bautizar nada para jugar un rato, y una sala sin nombre es
/// indistinguible de otra en la bandeja y en la lista de invitaciones.
///
/// Vive en `domain` y no en `infra` porque es una regla del producto, no un
/// dato que venga de ningún sitio. Estaba en el repositorio falso, y eso
/// obligaba a la pantalla a importar la implementación para pedir un nombre.
abstract final class RoomNames {
  static const List<String> pool = <String>[
    'La Guarida',
    'El Sótano',
    'Cueva de Pana',
    'El Refugio',
    'Punto de Encuentro',
  ];

  /// El `Random` se inyecta para poder fijar el resultado en un test.
  static String suggest([Random? random]) =>
      pool[(random ?? Random()).nextInt(pool.length)];
}
