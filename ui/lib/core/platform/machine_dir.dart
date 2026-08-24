import 'dart:io';

/// Dónde guarda Kanpachi lo que recuerda, cuando nadie se lo ha dicho.
///
/// # Un solo ámbito, el de la máquina
///
/// Todo lo que Kanpachi recuerda es de la máquina: la sala, la identidad, el
/// registro, el nombre, y también los ajustes de esta ventana. No hay ámbito de
/// persona, y el motivo no es de orden.
///
/// Una sala ES la máquina: un adaptador virtual llamado `kanpachi0`, un proceso
/// del motor y un juego de reglas de firewall, y de cada cosa hay UNA. El
/// daemon además no sabe qué usuario le está hablando —en Windows el pipe
/// concede al usuario interactivo y nadie impersona a nadie— así que un estado
/// por persona parecería separación sin serlo. Ver la decisión 42 de
/// `docs/02-decisiones-de-diseno.md`.
///
/// # Por qué esto existe y no se pregunta en el sitio
///
/// Porque la respuesta cambia con el sistema y estaba escrita a mano, con
/// `ProgramData` y barras invertidas, repartida por ficheros que no tratan de
/// eso. El lado Go lleva desde siempre un fichero por sistema con etiquetas de
/// compilación, en `daemon/paths`; esto es lo mismo de este lado.
///
/// **Lo que esto NO resuelve** es que la ventana arranque en Linux. Eso lo
/// bloquea el canal, que en Windows es un named pipe y en Linux un socket que
/// hoy solo acepta a root. Ver `daemon/transport/pipe/pipe_linux.go`.
abstract final class MachineDir {
  /// El nombre de la carpeta, donde el sistema usa uno.
  static const String folder = 'Kanpachi';

  /// El directorio de datos por omisión, o null si este sistema no tiene uno.
  ///
  /// Null es una respuesta legítima y no un fallo: lo normal es que el daemon
  /// lo diga con `--data` al lanzar la ventana, y esto solo contesta cuando
  /// nadie lo dijo. Quien lo reciba decide qué hacer con la ausencia.
  ///
  /// Las tres rutas son las mismas que contesta `daemon/paths`, y tienen que
  /// seguir siéndolo: el daemon escribe `api.token` ahí y esta ventana lo lee.
  static String? get defaultPath {
    if (Platform.isWindows) {
      final String base =
          Platform.environment['ProgramData'] ?? r'C:\ProgramData';
      return join(base, folder);
    }
    if (Platform.isLinux) return '/var/lib/kanpachi';
    if (Platform.isMacOS) return '/Library/Application Support/$folder';
    return null;
  }

  /// Pega un nombre a una carpeta con el separador de este sistema.
  ///
  /// Existe porque las rutas de este árbol se armaban con `\` literal, que
  /// fuera de Windows deja de ser un separador y pasa a ser parte del nombre
  /// del archivo. Es un fallo que no avisa: el archivo se crea, con un nombre
  /// absurdo, y nadie lo encuentra después.
  static String join(String base, String name) =>
      '$base${Platform.pathSeparator}$name';
}
