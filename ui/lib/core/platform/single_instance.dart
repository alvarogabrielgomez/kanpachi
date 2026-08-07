import 'dart:ffi';
import 'dart:isolate';

import 'package:ffi/ffi.dart';
import 'package:win32/win32.dart';

/// Una sola ventana de Kanpachi por sesión de Windows.
///
/// # Por qué hace falta, y no es cosmético
///
/// Tres cosas dependen de esto y ninguna es "que no salgan dos ventanas":
///
///  - **El daemon abre la ventana lanzando el ejecutable otra vez.** Es lo que
///    hace `uihost.Show` del lado de Go, y funciona porque el proceso nuevo se
///    topa con esto, avisa al que ya está, y se muere. Sin instancia única,
///    "abrir Kanpachi" con Kanpachi abierto daría una segunda ventana hablando
///    con el mismo daemon.
///  - **Los enlaces `kanpachi://`.** Windows abre un proceso nuevo por cada
///    enlace. Sin una forma de pasarle el código al que ya está, unirse desde
///    un enlace es imposible.
///  - **El icono de la bandeja.** Dos procesos ponen dos iconos, y el segundo
///    sobrevive al primero.
///
/// # Cómo, y por qué UN objeto y no dos
///
/// Con un evento con nombre, que hace las dos cosas a la vez. `CreateEvent`
/// sobre un nombre que ya existe devuelve un handle al MISMO objeto y deja
/// `ERROR_ALREADY_EXISTS`, así que crear ya responde "¿hay otra?" sin necesitar
/// un mutex aparte. Y ese mismo handle es por donde se le avisa.
///
/// El nombre va en `Local\`, que es por sesión de inicio de sesión. Dos
/// personas con sesión abierta en la misma máquina tienen cada una su Kanpachi,
/// que es lo correcto: la bandeja de una no es la de la otra.
///
/// Si el primer proceso muere, el último handle se cierra y el objeto
/// desaparece solo. El siguiente arranque vuelve a ser el primero, sin dejar
/// nada que limpiar. Un fichero de bloqueo tendría justo el problema contrario.
abstract final class SingleInstance {
  /// Reclama ser la única instancia.
  ///
  /// Devuelve `true` si esta es la primera. Devuelve `false` si ya había otra,
  /// y en ese caso **ya le avisó** para que enseñe su ventana: lo único que
  /// queda por hacer es salir.
  ///
  /// [onShowRequested] solo se llama en la primera, cada vez que otro proceso
  /// pide que la ventana se vea.
  static bool claim({
    required String eventName,
    required void Function() onShowRequested,
  }) {
    // Reservado y liberado con el MISMO asignador, dicho en voz alta: mezclar
    // los dos funciona todos los días y corrompe el montón el día malo.
    final Pointer<Utf16> nombre = eventName.toNativeUtf16(allocator: malloc);
    try {
      // Manual y sin señalar. Manual porque quien espera lo rearma a mano tras
      // atenderlo, y con automático dos avisos muy seguidos podrían colapsarse
      // en uno mientras se atiende el primero.
      final Win32Result<HANDLE> creado = CreateEvent(
        null,
        true,
        false,
        PCWSTR(nombre),
      );

      if (!creado.value.isValid) {
        // No se pudo ni crear ni abrir. Se sigue como si fuéramos la primera:
        // quedarse sin arrancar por no poder coordinarse sería peor que dos
        // ventanas, que es el peor caso de equivocarse por acá.
        return true;
      }
      final HANDLE handle = creado.value;

      if (creado.error == ERROR_ALREADY_EXISTS) {
        // Ya hay una. Se le avisa y este proceso sobra.
        SetEvent(handle);
        CloseHandle(handle);
        return false;
      }

      // Somos la primera. El handle NO se cierra: mientras viva, el objeto
      // existe y el siguiente arranque sabe que hay alguien.
      _escuchar(handle.address, onShowRequested);
      return true;
    } finally {
      malloc.free(nombre);
    }
  }

  /// Deja un isolate esperando avisos.
  ///
  /// En un isolate y no en un `Timer`, por lo mismo que el transporte del pipe:
  /// `WaitForSingleObject` PARA el hilo, y hacerlo en el de la interfaz
  /// congelaría la ventana. Con espera de verdad en vez de sondeo, un aviso se
  /// atiende al momento y sin gastar nada mientras no llega.
  static void _escuchar(int handle, void Function() onShowRequested) {
    final ReceivePort desdeElIsolate = ReceivePort();
    desdeElIsolate.listen((Object? _) => onShowRequested());

    // Sin `onExit` ni `onError`: este isolate vive lo que vive el proceso, y no
    // hay nada que hacer si muere salvo perder la capacidad de traer la ventana
    // al frente. La app sigue siendo usable.
    Isolate.spawn<_EsperaConfig>(
      _esperarAvisos,
      _EsperaConfig(handle: handle, aviso: desdeElIsolate.sendPort),
      debugName: 'kanpachi-instancia-unica',
    );
  }
}

/// Lo que el isolate necesita.
///
/// El handle viaja como entero porque los handles del kernel son de todo el
/// proceso, así que su número vale igual en cualquier isolate. Un `Pointer` no
/// se puede mandar y un `HANDLE` tampoco.
class _EsperaConfig {
  const _EsperaConfig({required this.handle, required this.aviso});

  final int handle;
  final SendPort aviso;
}

/// El bucle del isolate: esperar, avisar, rearmar.
void _esperarAvisos(_EsperaConfig cfg) {
  final HANDLE handle = HANDLE(Pointer.fromAddress(cfg.handle));
  while (true) {
    final Win32Result<WAIT_EVENT> espera = WaitForSingleObject(
      handle,
      INFINITE,
    );
    if (espera.value != WAIT_OBJECT_0) {
      // El objeto se cerró o pasó algo que no se entiende. Se sale en vez de
      // girar: un bucle que falla y reintenta sobre un handle muerto quema una
      // CPU entera sin que nadie lo note.
      return;
    }
    // Rearmar ANTES de avisar. Al revés, un aviso que llegue mientras la
    // interfaz atiende el anterior se perdería.
    ResetEvent(handle);
    cfg.aviso.send(null);
  }
}
