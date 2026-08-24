import 'dart:io';

import 'package:kanpachi_ui/core/platform/user_dir.dart';

/// El registro de ESTA ventana, en un archivo al lado del del daemon.
///
/// # Por qué existe
///
/// Porque la interfaz se muere sola y no dejaba nada. Medido el 2026-08-09 con
/// el bundle portable corriendo doce horas: **dieciocho muertes**, una cada
/// veinte a noventa minutos, y lo único que quedaba de cada una era una línea
/// del daemon diciendo que la relanzaba. Ni el error, ni si llegó a arrancar.
///
/// Un proceso de Flutter compilado para Windows no tiene dónde contarlo. La
/// salida estándar de un binario gráfico no va a ninguna parte, `stderr`
/// tampoco, y las dos vías obvias para redirigirlas están cerradas:
///
///  - **Heredar el stderr desde el daemon no se puede.** El daemon vive en la
///    sesión 0 y esta ventana en la del usuario, y los handles no se heredan
///    entre sesiones. Está escrito en `daemon/adapter/uihost/spawn_windows.go`,
///    en el `false` de `CreateProcessAsUser`.
///  - **Redirigir los streams del runner de C++ tampoco.** `flutter_windows.dll`
///    enlaza su propio CRT, así que un `freopen_s` en `windows/runner/` no toca
///    los del motor. Para eso existe `FlutterDesktopResyncOutputStreams`, y esa
///    API está cableada a `CONOUT$`: llamarla después de redirigir a un archivo
///    deshace la redirección.
///
/// Así que se escribe desde Dart, que es donde el error se puede leer.
///
/// **Lo que esto NO ve:** un fallo nativo, del motor de Flutter, de un plugin o
/// del driver de vídeo, no pasa por Dart y no llega acá. Esa mitad la cubre el
/// código de salida que el daemon anota al ver morir el proceso.
abstract final class AppLog {
  /// Cuánto se deja crecer el archivo antes de empezar de cero.
  ///
  /// La ventana puede estar semanas abierta, y esto se escribe sin buffer.
  /// Cuando se pasa, lo de antes se conserva como `.old`, o sea que siempre
  /// hay dos tramos: el de ahora y el anterior. El daemon rota igual.
  static const int _maxBytes = 1024 * 1024;

  static const String _fileName = 'kanpachi-ui.log';

  static File? _file;

  /// Dónde quedó, o null si no se pudo escribir en ningún sitio.
  static String? get path => _file?.path;

  /// Abre el archivo y deja el registro listo. Idempotente.
  ///
  /// `dir` es lo que dijo el daemon con `--log`, y es la carpeta donde él deja
  /// el suyo. `fallback` es a dónde ir si esa no se puede escribir, y ese caso
  /// no es raro: el daemon corre como SYSTEM y esta ventana no, y el permiso
  /// que `C:\ProgramData` hereda a sus subcarpetas deja a los usuarios crear
  /// carpetas y NO crear archivos. Sin la segunda oportunidad, el producto
  /// instalado se quedaría sin registro y sin decirlo.
  static void open({String? dir, String? fallback}) {
    if (_file != null) return;
    for (final String? candidate in <String?>[dir, fallback, UserDir.path]) {
      if (candidate == null || candidate.isEmpty) continue;
      final File? opened = _tryOpen(candidate);
      if (opened != null) {
        _file = opened;
        return;
      }
    }
  }

  /// Anota una línea, con el mismo formato que el log del daemon.
  ///
  /// **Síncrono y sin buffer, a propósito.** Lo que interesa de este archivo es
  /// la última línea antes de morir, y un buffer es exactamente lo que se
  /// pierde cuando el proceso se va sin cerrar nada.
  ///
  /// No lanza nunca. Un log que puede tumbar la app que registra es peor que no
  /// tener log, y este corre dentro del manejador de errores.
  static void write(String level, String message, [String? detail]) {
    final File? f = _file;
    if (f == null) return;
    final String line = detail == null
        ? '${_stamp()} $level $message\n'
        : '${_stamp()} $level $message [$detail]\n';
    try {
      f.writeAsStringSync(line, mode: FileMode.append, flush: true);
    } on Object {
      // Se ignora a propósito, y no queda nada que hacer con ello: el disco
      // lleno o el archivo tomado por otro no se arreglan desde acá, y el único
      // canal para contarlo es el que acaba de fallar.
    }
  }

  static void info(String message, [String? detail]) =>
      write('info ', message, detail);

  /// Algo salió mal y la ventana sigue funcionando sin ello.
  ///
  /// Existe porque `debugPrint` no imprime en una compilación de release, y lo
  /// que se estaba tragando así era un ajuste que no se guardaba nunca: el
  /// producto instalado lo hizo durante meses sin dejar una sola línea. Un fallo
  /// que solo se ve desde el depurador es un fallo que no se ve.
  static void warn(String message, [String? detail]) =>
      write('warn ', message, detail);

  /// Anota un error con su traza, que es lo que esto existe para guardar.
  ///
  /// El origen se dice porque son tres puertas distintas y no fallan por lo
  /// mismo: la zona, el framework y el despachador de la plataforma.
  static void error(String origin, Object error, StackTrace? stack) {
    write('error', 'la interfaz falló', 'origen $origin error $error');
    final String traza = stack?.toString().trim() ?? '';
    if (traza.isEmpty) {
      // **Se dice, en vez de escribir `traza []` y parecer que hay una.**
      //
      // Una traza vacía no es ruido: significa que el error se completó a mano
      // sin pasarle una, y eso ya costó una investigación entera el 2026-08-09
      // por leerse como si la traza simplemente no se hubiera capturado.
      write('error', 'sin traza', 'el error se completó sin pasar una');
      return;
    }
    write('error', 'traza', traza.replaceAll('\n', ' | '));
  }

  /// `2026-08-09 10:35:12.320`, igual que el daemon, para poder leer los dos
  /// archivos con las líneas intercaladas por hora.
  static String _stamp() {
    final String raw = DateTime.now().toString();
    return raw.length >= 23 ? raw.substring(0, 23) : raw;
  }

  /// Intenta dejar el archivo listo en esa carpeta, y contesta null si no pudo.
  ///
  /// Escribe de verdad antes de dar la carpeta por buena: `Directory.create`
  /// puede salir bien en un sitio donde después no se pueda crear un archivo,
  /// que es justo la forma del permiso de `C:\ProgramData`.
  static File? _tryOpen(String dir) {
    try {
      Directory(dir).createSync(recursive: true);
      final File f = File('$dir${Platform.pathSeparator}$_fileName');
      _rotateIfBig(f);
      f.writeAsStringSync('', mode: FileMode.append, flush: true);
      return f;
    } on Object {
      return null;
    }
  }

  static void _rotateIfBig(File f) {
    try {
      if (!f.existsSync() || f.lengthSync() < _maxBytes) return;
      final File old = File('${f.path}.old');
      if (old.existsSync()) old.deleteSync();
      f.renameSync(old.path);
    } on Object {
      // Un archivo que no se puede rotar se sigue usando: crecer de más es
      // menos malo que quedarse sin registro.
    }
  }
}
