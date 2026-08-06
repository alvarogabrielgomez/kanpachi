import 'dart:async';
import 'dart:ffi';
import 'dart:isolate';
import 'dart:typed_data';

import 'package:ffi/ffi.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_transport.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/pipe_names.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/pipe_workers.dart';
import 'package:win32/win32.dart';

/// The daemon's named pipe, spoken over `dart:ffi`.
///
/// # Why this exists at all
///
/// `dart:io` does not speak Windows named pipes, and the old shortcut of
/// opening one as a file has been broken since Flutter 3.27. So the only road
/// is kernel32: `CreateFileW`, `ReadFile`, `WriteFile`.
///
/// # The part that surprises people
///
/// **An unprivileged process CAN open this pipe.** The `ProtectedPrefix\
/// Administrators\` prefix restricts who may CREATE the name, not who may open
/// it, and the daemon's descriptor grants the interactive user exactly
/// read plus write plus synchronise. The UI runs without privileges and that is
/// the design, not a leak.
///
/// # Shape
///
/// The handle is owned here and shared with two worker isolates, one reading
/// and one writing, because an isolate parked in a native call cannot drain its
/// own inbox. See `pipe_workers.dart` for why the handle has to be overlapped.
class WindowsPipeTransport implements DaemonTransport {
  WindowsPipeTransport({String? name, this.busyRetries = 3})
    : name = name ?? PipeNames.defaultName;

  /// Which pipe. See [PipeNames.defaultName]: production unless this build was
  /// compiled to talk to a `--console` daemon.
  final String name;

  /// How many times to come back when the daemon has all its instances busy.
  /// `MaxConns` is 8 on the other side, so this is a real case with several
  /// windows open, and it clears in milliseconds.
  final int busyRetries;

  final StreamController<List<int>> _entrada =
      StreamController<List<int>>.broadcast();
  final Map<int, Completer<void>> _escrituras = <int, Completer<void>>{};

  Isolate? _lector;
  Isolate? _escritor;
  ReceivePort? _buzon;
  SendPort? _alEscritor;

  int _pipe = 0;
  int _stop = 0;
  int _seq = 0;
  int _despedidas = 0;
  bool _abierto = false;
  bool _cerrando = false;

  /// Completes when BOTH workers have said they are done touching the handle.
  final Completer<void> _ambosFuera = Completer<void>();

  @override
  Stream<List<int>> get incoming => _entrada.stream;

  @override
  Future<void> connect() async {
    if (_abierto) return;

    await _abrir();

    final Completer<SendPort> puertoDelEscritor = Completer<SendPort>();
    final ReceivePort buzon = ReceivePort();
    _buzon = buzon;
    buzon.listen((Object? m) => _delWorker(m, puertoDelEscritor));

    final PipeWorkerConfig cfg = PipeWorkerConfig(
      pipeHandle: _pipe,
      stopEvent: _stop,
      toOwner: buzon.sendPort,
    );

    _lector = await Isolate.spawn(
      pipeReader,
      cfg,
      debugName: 'kanpachi-pipe-r',
    );
    _escritor = await Isolate.spawn(
      pipeWriter,
      cfg,
      debugName: 'kanpachi-pipe-w',
    );

    _alEscritor = await puertoDelEscritor.future.timeout(
      const Duration(seconds: 5),
      onTimeout: () => throw const DaemonUnreachable(
        'el isolate que escribe en el pipe no arrancó',
        kind: DaemonUnreachableKind.notConnected,
      ),
    );
    _abierto = true;
  }

  Future<void> _abrir() async {
    // Allocated and freed with the SAME allocator, spelled out. `toNativeUtf16`
    // defaults to one of them and freeing with the other is the kind of thing
    // that works every day and corrupts the heap on the bad one.
    final Pointer<Utf16> ruta = name.toNativeUtf16(allocator: malloc);
    try {
      for (int intento = 0; ; intento++) {
        // CreateFileW on a named pipe does not park: it connects, or it comes
        // back at once saying busy or not there. That is why it runs on this
        // isolate and only the reading and writing move away.
        final Win32Result<HANDLE> r = CreateFile(
          PCWSTR(ruta),
          GENERIC_READ | GENERIC_WRITE,
          FILE_SHARE_NONE,
          null,
          OPEN_EXISTING,
          FILE_FLAG_OVERLAPPED,
          null,
        );

        if (r.value.isValid) {
          _pipe = r.value.address;
          break;
        }

        if (r.error == ERROR_PIPE_BUSY && intento < busyRetries) {
          // Deliberately NOT WaitNamedPipe, which parks the calling thread, and
          // this one is the UI's. A few short waits cover the real case, which
          // is another window holding an instance, and they yield meanwhile.
          await Future<void>.delayed(const Duration(milliseconds: 120));
          continue;
        }

        throw DaemonUnreachable(
          _porQueNoAbrio(r.error),
          kind: DaemonUnreachableKind.notConnected,
        );
      }
    } finally {
      malloc.free(ruta);
    }

    final Win32Result<HANDLE> parada = CreateEvent(null, true, false, null);
    if (!parada.value.isValid) {
      CloseHandle(HANDLE(Pointer.fromAddress(_pipe)));
      _pipe = 0;
      throw DaemonUnreachable(
        'no se pudo crear el evento de parada, error ${parada.error} de Windows',
        kind: DaemonUnreachableKind.notConnected,
      );
    }
    _stop = parada.value.address;
  }

  /// The message for a failed open, which is the one the user meets most.
  String _porQueNoAbrio(int error) {
    if (error == ERROR_FILE_NOT_FOUND) {
      return 'el servicio de Kanpachi no está escuchando en $name';
    }
    if (error == ERROR_PIPE_BUSY) {
      return 'el servicio de Kanpachi tiene todas sus conexiones ocupadas';
    }
    if (error == ERROR_ACCESS_DENIED) {
      return 'Windows negó el acceso al canal de Kanpachi';
    }
    return 'no se pudo abrir el canal de Kanpachi, error $error de Windows';
  }

  void _delWorker(Object? mensaje, Completer<SendPort> puertoDelEscritor) {
    final List<Object?> m = mensaje! as List<Object?>;
    switch (m[0]) {
      case PipeMsg.port:
        if (!puertoDelEscritor.isCompleted) {
          puertoDelEscritor.complete(m[1]! as SendPort);
        }
      case PipeMsg.data:
        if (!_entrada.isClosed) _entrada.add(m[1]! as Uint8List);
      case PipeMsg.ack:
        _escrituras.remove(m[1]! as int)?.complete();
      case PipeMsg.fail:
        _escrituras
            .remove(m[1]! as int)
            ?.completeError(
              DaemonUnreachable(
                m[2]! as String,
                kind: DaemonUnreachableKind.writeFailed,
              ),
            );
      case PipeMsg.closed:
        _despedidas++;
        if (_despedidas >= 2 && !_ambosFuera.isCompleted) {
          _ambosFuera.complete();
        }
        // The reader hanging up is how the death of the daemon arrives. Closing
        // the stream lands in DaemonClient's onDone, which breaks every pending
        // request at once instead of leaving them to expire one by one.
        if (!_cerrando && !_entrada.isClosed) _entrada.close();
    }
  }

  @override
  Future<void> send(List<int> bytes) {
    final SendPort? destino = _alEscritor;
    if (!_abierto || destino == null) {
      throw const DaemonUnreachable(
        'el canal con el daemon no está abierto',
        kind: DaemonUnreachableKind.notConnected,
      );
    }

    final int seq = ++_seq;
    final Completer<void> espera = Completer<void>();
    _escrituras[seq] = espera;
    destino.send(<Object?>[PipeMsg.write, seq, Uint8List.fromList(bytes)]);
    return espera.future;
  }

  @override
  Future<void> close() async {
    if (_cerrando) return;
    _cerrando = true;

    // Setting the stop event is the whole shutdown: both workers are waiting on
    // it alongside their own I/O, so neither one depends on the daemon ever
    // saying anything again. The writer also gets a message, because when it is
    // idle it sits on its inbox and not on the wait.
    if (_stop != 0) SetEvent(HANDLE(Pointer.fromAddress(_stop)));
    _alEscritor?.send(<Object?>[PipeMsg.stop]);

    for (final Completer<void> c in _escrituras.values) {
      if (!c.isCompleted) {
        c.completeError(
          const DaemonUnreachable(
            'el canal se cerró mientras se escribía',
            kind: DaemonUnreachableKind.writeFailed,
          ),
        );
      }
    }
    _escrituras.clear();

    // WAIT for both workers to say they are done, and only then take the
    // handle away. This is not politeness: each worker owns an OVERLAPPED and
    // a pending operation on this very handle, and closing it underneath them
    // is a use-after-free that shows up as an access violation somewhere else
    // entirely. A fixed grace period would be a guess, and a guess that is
    // usually right is the worst kind of race.
    //
    // Bounded anyway, because a close that CAN hang is a close that will hang
    // the app on exit. If the grace runs out the kill below is the fallback,
    // and a worker still inside a native call takes the process down with the
    // handle leaked, which is strictly better than corrupting it.
    if (_lector != null || _escritor != null) {
      await _ambosFuera.future.timeout(
        const Duration(seconds: 3),
        onTimeout: () {},
      );
    }

    _lector?.kill(priority: Isolate.immediate);
    _escritor?.kill(priority: Isolate.immediate);
    _lector = null;
    _escritor = null;

    if (_pipe != 0) {
      CloseHandle(HANDLE(Pointer.fromAddress(_pipe)));
      _pipe = 0;
    }
    if (_stop != 0) {
      CloseHandle(HANDLE(Pointer.fromAddress(_stop)));
      _stop = 0;
    }

    _buzon?.close();
    _buzon = null;
    _alEscritor = null;
    _abierto = false;

    if (!_entrada.isClosed) await _entrada.close();
  }
}
