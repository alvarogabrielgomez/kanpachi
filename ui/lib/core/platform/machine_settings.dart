/// Lo que esta máquina recuerda de cómo se presenta.
///
/// Es el espejo del `profile.json` que escribe el daemon, menos el nombre, que
/// tiene su propio método porque se valida y deriva una sugerencia.
///
/// # Por qué vive en `core` y no en la feature de sesión
///
/// Porque lo usan tres features que no se conocen entre sí —el shell guarda el
/// tamaño, la sesión la narración, la de actualización el aviso de versión— y
/// `core` es lo transversal. El candado de `import_purity_test` lo dice al
/// revés y es lo mismo: `core` no puede importar una feature, así que lo que
/// comparten varias tiene que estar acá.
///
/// # Sin `Size`, a propósito
///
/// Dos enteros y no un tipo de `dart:ui`, para que esto no arrastre Flutter a
/// ningún sitio: lo importa también el dominio de la sesión, que tiene prohibido
/// conocerlo. Quien dibuja convierte.
class MachineSettings {
  const MachineSettings({
    this.verbose = false,
    this.windowWidth = 0,
    this.windowHeight = 0,
    this.pendingUpdate = '',
  });

  /// Si las caras narran, paso a paso, lo que hace el daemon.
  final bool verbose;

  /// El tamaño que tenía la ventana al cerrarla. Cero en los dos es que nadie
  /// la ha cambiado nunca.
  final int windowWidth;
  final int windowHeight;

  /// Una versión publicada más nueva que la que corre, encontrada por la cara
  /// que preguntara última. Vacío cuando no se sabe de ninguna.
  final String pendingUpdate;

  /// Si hay un tamaño de verdad, o null.
  ///
  /// **Los dos o ninguno**, que es la misma invariante que aplica el daemon:
  /// media medida no es una medida, y devolver un ancho con un alto en cero
  /// daría una ventana que nadie tuvo nunca.
  bool get hasWindowSize => windowWidth > 0 && windowHeight > 0;

  MachineSettings copyWith({
    bool? verbose,
    int? windowWidth,
    int? windowHeight,
    String? pendingUpdate,
  }) => MachineSettings(
    verbose: verbose ?? this.verbose,
    windowWidth: windowWidth ?? this.windowWidth,
    windowHeight: windowHeight ?? this.windowHeight,
    pendingUpdate: pendingUpdate ?? this.pendingUpdate,
  );

  @override
  bool operator ==(Object other) =>
      other is MachineSettings &&
      other.verbose == verbose &&
      other.windowWidth == windowWidth &&
      other.windowHeight == windowHeight &&
      other.pendingUpdate == pendingUpdate;

  @override
  int get hashCode =>
      Object.hash(verbose, windowWidth, windowHeight, pendingUpdate);

  @override
  String toString() =>
      'MachineSettings(verbose: $verbose, window: ${windowWidth}x$windowHeight, '
      'pendingUpdate: $pendingUpdate)';
}

/// Quién sabe guardar un ajuste, visto desde acá.
///
/// Una función y no una interfaz porque es una sola operación, y así `core` no
/// necesita conocer al repositorio de la sesión, que vive en una feature y le
/// está prohibido. Lo ausente no se toca, igual que en el método del cable.
typedef SaveSettings =
    Future<MachineSettings> Function({
      bool? verbose,
      int? windowWidth,
      int? windowHeight,
      String? pendingUpdate,
    });
