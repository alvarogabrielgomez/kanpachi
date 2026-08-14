/// Lo que identifica a QUIEN publicó este binario, en un solo sitio.
///
/// # Por qué existe, y por qué está duplicado con Go
///
/// Dart no puede importar una constante de Go, así que el repositorio y el
/// interruptor de actualizaciones viven dos veces: acá y en
/// `internal/selfupdate`. Duplicarlos a la vista, en un fichero que se llama
/// como lo que contiene, es lo que hace posible el candado que los compara; el
/// fallo que se está evitando es un fork que edita la constante de Go y sigue
/// publicando una ventana que apunta al repositorio original.
///
/// # Lo que NO va acá, y hay que decirlo
///
/// Nada que las dos máquinas de una sala tengan que calcular igual. Los
/// parámetros de derivación, el alfabeto del invite ID y las versiones fijadas
/// parecen configuración y no lo son: están congelados porque los dos lados los
/// calculan por separado y sin hablarse, y un fork que los "configure" produce
/// salas donde la gente pega el mismo código y se queda sola, sin ningún síntoma
/// que apunte a la causa. Esto es de quien publica, jamás del protocolo.
abstract final class Brand {
  /// El repositorio del que salen las publicaciones. Espejo de
  /// `internal/selfupdate.Repo`.
  static const String repo = 'alvarogabrielgomez/kanpachi';

  /// Si esta copia comprueba versiones. Espejo de `internal/selfupdate.Enabled`.
  ///
  /// En `false` no se consulta nada y la ventana se calla. Sin este interruptor
  /// la única salida para un fork que publica por otro canal sería dejar [repo]
  /// apuntando a un repositorio que no existe, y eso no apaga la comprobación:
  /// la convierte en un fallo por cada vez que alguien la pide.
  static const bool updatesEnabled = true;

  /// Lo que contesta qué versión hay publicada.
  static const String latestApi =
      'https://api.github.com/repos/$repo/releases/latest';

  /// La página a la que se manda a alguien a descargar.
  static const String releases = 'https://github.com/$repo/releases/latest';

  /// Dónde se explica el producto. Espejo de `internal/brand.Docs`.
  ///
  /// El repositorio, y no un dominio: un dominio es de alguien que paga un DNS,
  /// y el repositorio es de donde salió esto.
  static const String docs = 'https://github.com/$repo';

  /// Dónde se explica el servidor de encuentro, con calma y entero.
  ///
  /// Es el destino de los «Más información» de las tres pantallas que hablan de
  /// un seed. Apuntaban a `docs` con un ancla `#seed` que no existía en ningún
  /// sitio: el enlace abría el README y quien lo pulsaba se quedaba igual.
  ///
  /// **Un fichero del repositorio, y en inglés**, como
  /// `kanpachi-protection.md`: se enlaza desde fuera, y lo que hay dentro son
  /// afirmaciones sobre qué ve y qué guarda una máquina de un tercero. Los ocho
  /// documentos de diseño siguen en castellano, en `docs/`.
  static const String seedDoc =
      'https://github.com/$repo/blob/main/kanpachi-seed.md';
}
