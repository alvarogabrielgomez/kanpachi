/// The two worker isolates that do the blocking half of the named pipe.
///
/// # Why there are two, and why they block
///
/// An isolate parked inside a native call cannot drain its own `ReceivePort`:
/// messages are delivered by the Dart event loop and the event loop is not
/// running. So one isolate cannot both sit waiting for the daemon to say
/// something and accept "here are bytes to write". Hence one reader and one
/// writer, over the SAME handle.
///
/// # Why the handle is overlapped
///
/// Because a synchronous handle would deadlock this design on the first call,
/// and that is a Windows fact and not a Dart one: the kernel serialises I/O on
/// a synchronous file object, so a `ReadFile` parked waiting for the answer
/// holds the file object and the `WriteFile` carrying the question queues
/// behind it. With `FILE_FLAG_OVERLAPPED` both run at once. It is also what
/// go-winio does on the daemon side, for the same reason.
///
/// The isolates still block. They block in `WaitForMultipleObjects` instead of
/// inside the I/O call, which is identical from Dart's point of view and much
/// better from the kernel's: a second handle in that wait is the stop event, so
/// closing does not depend on the daemon ever saying anything again.
library;

import 'dart:ffi';
import 'dart:isolate';
import 'dart:typed_data';

import 'package:ffi/ffi.dart';
import 'package:win32/win32.dart';

/// Tags of the messages the workers post back. Primitives only, so nothing here
/// can stop being sendable.
abstract final class PipeMsg {
  /// `['data', Uint8List]`
  static const String data = 'data';

  /// `['closed', int win32Error]` — 0 when it was our own stop.
  static const String closed = 'closed';

  /// `['port', SendPort]` — the writer handing over its inbox.
  static const String port = 'port';

  /// `['ack', int seq]` and `['fail', int seq, String reason]`
  static const String ack = 'ack';
  static const String fail = 'fail';

  /// `['write', int seq, Uint8List]` and `['stop']`, towards the writer.
  static const String write = 'write';
  static const String stop = 'stop';
}

/// What crosses to a worker at spawn time. Addresses as plain ints: kernel
/// handles belong to the process, and Dart isolates are threads inside one.
class PipeWorkerConfig {
  const PipeWorkerConfig({
    required this.pipeHandle,
    required this.stopEvent,
    required this.toOwner,
  });

  final int pipeHandle;
  final int stopEvent;
  final SendPort toOwner;
}

/// One read buffer. 64 KiB matches the pipe buffer the daemon asks go-winio
/// for, so a full read never needs two trips.
const int _bufferBytes = 64 * 1024;

/// Reads from the pipe until the daemon stops talking or the owner says stop.
void pipeReader(PipeWorkerConfig cfg) {
  final HANDLE pipe = HANDLE(Pointer.fromAddress(cfg.pipeHandle));
  final HANDLE stop = HANDLE(Pointer.fromAddress(cfg.stopEvent));

  final Pointer<Uint8> buffer = calloc<Uint8>(_bufferBytes);
  final Pointer<OVERLAPPED> ov = calloc<OVERLAPPED>();
  final Pointer<Uint32> moved = calloc<Uint32>();
  final Pointer<Pointer> waits = calloc<Pointer>(2);

  final Win32Result<HANDLE> evento = CreateEvent(null, true, false, null);
  if (!evento.value.isValid) {
    calloc
      ..free(buffer)
      ..free(ov)
      ..free(moved)
      ..free(waits);
    cfg.toOwner.send(<Object?>[PipeMsg.closed, evento.error]);
    return;
  }
  final HANDLE readEvent = evento.value;

  waits[0] = readEvent;
  waits[1] = stop;

  int motivo = 0;
  // Si la cancelación la pedimos NOSOTROS, que es el camino de parada de abajo.
  //
  // **Sin esta bandera, `ERROR_OPERATION_ABORTED` se daba por propio siempre**,
  // y eso tapaba la única prueba del fallo que se está persiguiendo: Windows
  // cancela la E/S pendiente de un hilo cuando ese hilo termina, textual en la
  // documentación de `ExitThread` —«all pending I/O initiated by the thread that
  // is not associated with a completion port is canceled»—, y esta E/S va con
  // evento y no con puerto de finalización. Un isolate de Dart no tiene hilo
  // propio garantizado: corre sobre el pool de la VM.
  //
  // O sea que un aborto que NO pedimos significa que el hilo que lanzó la
  // lectura se fue, y eso se ve desde fuera como un canal que se muere solo sin
  // que el daemon haya cerrado nada. Que es exactamente lo medido el
  // 2026-08-09: doce veces, y cero cortes del lado del daemon.
  bool abortoPropio = false;
  // El hilo que lanza la primera lectura, para poder comparar con el de la
  // última. Si cambian, la migración está probada y no hay que deducirla.
  final int hiloAlEmpezar = GetCurrentThreadId();
  try {
    while (true) {
      // ReadFile puts the event in the non-signalled state itself before it
      // starts, so there is no ResetEvent here and there must not be one.
      ov.ref.hEvent = readEvent;
      final Win32Result<bool> arranque = ReadFile(
        pipe,
        buffer,
        _bufferBytes,
        null,
        ov,
      );

      if (!arranque.value && arranque.error != ERROR_IO_PENDING) {
        motivo = arranque.error;
        break;
      }

      final Win32Result<WAIT_EVENT> espera = WaitForMultipleObjects(
        2,
        waits,
        false,
        INFINITE,
      );

      if (espera.value != WAIT_OBJECT_0) {
        // Either the owner asked to stop, or the wait itself failed. Both end
        // the same way, and the pending read has to be reaped before anybody
        // frees the OVERLAPPED or closes the handle.
        abortoPropio = true;
        CancelIoEx(pipe, ov);
        GetOverlappedResult(pipe, ov, moved, true);
        break;
      }

      final Win32Result<bool> hecho = GetOverlappedResult(
        pipe,
        ov,
        moved,
        false,
      );
      if (!hecho.value) {
        motivo = hecho.error;
        // **ERROR_IO_INCOMPLETE no es un fallo: es "el kernel todavía es dueño
        // de este OVERLAPPED".** Salir sin más lo dejaba libre en el `finally`
        // de abajo, con una lectura viva apuntando a esos 32 bytes. Cuando esa
        // lectura termina, el kernel escribe su estado en memoria que ya es de
        // otro, y eso revienta MÁS TARDE y EN OTRO SITIO. Es la forma exacta de
        // los 0xC0000005 que se midieron el 2026-08-09.
        //
        // Se cobra la operación antes de irse, igual que en el camino de parada
        // unas líneas más arriba: cancelar y esperar de verdad al resultado es
        // lo único que garantiza que nadie más toque este puntero.
        if (motivo == ERROR_IO_INCOMPLETE) {
          CancelIoEx(pipe, ov);
          GetOverlappedResult(pipe, ov, moved, true);
        }
        break;
      }

      final int n = moved.value;
      // Zero bytes on a message-less byte pipe means the other end is done.
      if (n == 0) break;

      cfg.toOwner.send(<Object?>[
        PipeMsg.data,
        Uint8List.fromList(buffer.asTypedList(n)),
      ]);
    }
  } finally {
    CloseHandle(readEvent);
    calloc
      ..free(buffer)
      ..free(ov)
      ..free(moved)
      ..free(waits);
  }

  // **Solo se traga el aborto que pedimos nosotros**, y esa distinción es la
  // prueba. Antes se tragaba cualquiera, con el argumento de que
  // `ERROR_OPERATION_ABORTED` solo lo deja nuestro `CancelIoEx`. Es falso: lo
  // deja también Windows al terminar el hilo que lanzó la lectura, y esta E/S
  // va con evento y no con puerto de finalización. Ver [abortoPropio].
  if (abortoPropio && motivo == ERROR_OPERATION_ABORTED) motivo = 0;
  cfg.toOwner.send(<Object?>[
    PipeMsg.closed,
    motivo,
    hiloAlEmpezar,
    GetCurrentThreadId(),
  ]);
}

/// Writes whatever the owner sends, one message at a time.
void pipeWriter(PipeWorkerConfig cfg) {
  final HANDLE pipe = HANDLE(Pointer.fromAddress(cfg.pipeHandle));
  final HANDLE stop = HANDLE(Pointer.fromAddress(cfg.stopEvent));

  final Pointer<OVERLAPPED> ov = calloc<OVERLAPPED>();
  final Pointer<Uint32> moved = calloc<Uint32>();
  final Pointer<Pointer> waits = calloc<Pointer>(2);

  final Win32Result<HANDLE> evento = CreateEvent(null, true, false, null);
  if (!evento.value.isValid) {
    calloc
      ..free(ov)
      ..free(moved)
      ..free(waits);
    cfg.toOwner.send(<Object?>[PipeMsg.closed, evento.error]);
    return;
  }
  final HANDLE writeEvent = evento.value;

  waits[0] = writeEvent;
  waits[1] = stop;

  final ReceivePort inbox = ReceivePort();
  cfg.toOwner.send(<Object?>[PipeMsg.port, inbox.sendPort]);

  void limpiar() {
    CloseHandle(writeEvent);
    calloc
      ..free(ov)
      ..free(moved)
      ..free(waits);
    inbox.close();
  }

  inbox.listen((Object? mensaje) {
    final List<Object?> m = mensaje! as List<Object?>;
    if (m[0] == PipeMsg.stop) {
      limpiar();
      cfg.toOwner.send(<Object?>[PipeMsg.closed, 0]);
      Isolate.exit();
    }

    final int seq = m[1]! as int;
    final Uint8List bytes = m[2]! as Uint8List;

    final Pointer<Uint8> buffer = calloc<Uint8>(bytes.length);
    buffer.asTypedList(bytes.length).setAll(0, bytes);
    try {
      ov.ref.hEvent = writeEvent;
      final Win32Result<bool> arranque = WriteFile(
        pipe,
        buffer,
        bytes.length,
        null,
        ov,
      );

      if (!arranque.value && arranque.error != ERROR_IO_PENDING) {
        cfg.toOwner.send(<Object?>[
          PipeMsg.fail,
          seq,
          'WriteFile falló con el error ${arranque.error} de Windows',
        ]);
        return;
      }

      final Win32Result<WAIT_EVENT> espera = WaitForMultipleObjects(
        2,
        waits,
        false,
        INFINITE,
      );
      if (espera.value != WAIT_OBJECT_0) {
        CancelIoEx(pipe, ov);
        GetOverlappedResult(pipe, ov, moved, true);
        cfg.toOwner.send(<Object?>[
          PipeMsg.fail,
          seq,
          'la escritura se canceló porque el transporte se estaba cerrando',
        ]);
        return;
      }

      final Win32Result<bool> hecho = GetOverlappedResult(
        pipe,
        ov,
        moved,
        false,
      );
      if (!hecho.value) {
        // Lo mismo que en el lector, y acá es peor: este `ov` NO se libera al
        // salir, se REUTILIZA en la escritura siguiente. Dejar una operación
        // viva encima significa que la próxima `WriteFile` pisa un OVERLAPPED
        // que el kernel todavía está usando.
        if (hecho.error == ERROR_IO_INCOMPLETE) {
          CancelIoEx(pipe, ov);
          GetOverlappedResult(pipe, ov, moved, true);
        }
        cfg.toOwner.send(<Object?>[
          PipeMsg.fail,
          seq,
          'la escritura no se completó, error ${hecho.error} de Windows',
        ]);
        return;
      }

      cfg.toOwner.send(<Object?>[PipeMsg.ack, seq]);
    } finally {
      calloc.free(buffer);
    }
  });
}
