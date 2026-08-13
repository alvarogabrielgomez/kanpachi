import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';

/// Qué se le enseña a una persona cuando algo que se le pidió al daemon falló.
///
/// # Qué reemplaza, y por qué es un defecto y no un detalle
///
/// Cuatro pantallas pintaban `e.toString()` en su hueco de error. Eso escribe
/// en la cara del usuario cosas como
/// `DaemonUnreachable(timedOut): el daemon no contestó a own_seed en 10 s`. Se
/// vio en la pantalla del servidor, donde ese hueco existe para decir qué tiene
/// de malo el nombre que la persona acaba de escribir: ahí un fallo de
/// transporte se lee como si el nombre fuera el problema, y lo que hace falta
/// hacer es lo contrario de corregirlo.
///
/// # Por qué vive acá y no en el catálogo
///
/// Porque las dos excepciones son del dominio de la sesión, y `core/` no puede
/// importar una feature: lo vigila `import_purity_test`. El catálogo sigue
/// siendo el dueño del texto de cada [FailureCode]; esto solo elige a cuál
/// preguntarle.
///
/// # Las dos mitades, que llevan a cosas distintas
///
/// Un [DaemonError] es el daemon contestando que no, con su código cerrado, y
/// su copy ya existe. Un [DaemonUnreachable] es no haber podido preguntar: no
/// tiene nada que ver con lo escrito y se cura reintentando, así que decirlo
/// como si fuera un rechazo manda a corregir algo que está bien.
String daemonFailureText(Object error) {
  if (error is DaemonError) {
    final FailureCode? code = error.resolved;
    if (code != null) return AppMessages.failure(code).body;
    // Un código que esta versión no conoce. Se dice que falló y no se inventa
    // la explicación: el código crudo ya queda en el log, que es donde sirve.
    return 'Kanpachi no pudo completar eso. Vuelve a intentarlo.';
  }
  if (error is DaemonUnreachable) {
    return 'El servicio de Kanpachi no contestó. No es por lo que escribiste: '
        'vuelve a intentarlo en un momento.';
  }
  return 'Algo falló y no se pudo completar. Vuelve a intentarlo.';
}
