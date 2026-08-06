import 'dart:async';

import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_client.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/daemon_transport.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/pipe_names.dart';
import 'package:kanpachi_ui/features/session/infra/daemon/pipe/windows_pipe_transport.dart';

/// Produces a live, greeted client, and throws the dead ones away.
///
/// # Why a connection cannot be reused forever
///
/// The daemon drops an idle connection after ten minutes. Anybody who looks at
/// the room, alt-tabs into a game for a round and comes back finds a link that
/// is already gone. With no polling anywhere in this app, that is not the
/// exceptional case: it is the NORMAL one, so it is designed for instead of
/// reported.
///
/// # Why a fresh client and not a reconnect
///
/// Because a [DaemonClient] is a one-shot object by construction: the greeting,
/// the request ids and the closed flag are per connection and none of them can
/// be reset. Adding a reconnect to it would be adding a second, hidden lifetime
/// to something that has one. So the client is made cheap to throw away, and
/// this is what throws it.
///
/// The token is re-read from disk on every connection. The daemon rotates it
/// once per process and deletes the file on exit, so one remembered across a
/// restart is always wrong, and the symptom would be an `unauthorized` nobody
/// can explain.
class DaemonConnector {
  DaemonConnector({String? pipeName, this.tokenPath})
    : _pipeName = pipeName ?? PipeNames.defaultName,
      _tokenLoader = null,
      _transportFactory = null;

  DaemonConnector.withTransport({
    required Future<String?> Function() tokenLoader,
    required DaemonTransport Function() transportFactory,
  }) : this._withTransport(tokenLoader, transportFactory);

  DaemonConnector._withTransport(this._tokenLoader, this._transportFactory)
    : _pipeName = PipeNames.defaultName,
      tokenPath = null;

  final String _pipeName;

  /// Overrides the two system boundaries while keeping the connector, client,
  /// codec and repository real.
  ///
  /// Production leaves both null. Protocol tests provide an in-memory
  /// transport and a token without putting a fake repository above the wire.
  /// The factory must return a fresh one-shot transport on every call.
  final Future<String?> Function()? _tokenLoader;
  final DaemonTransport Function()? _transportFactory;

  /// Dónde está `api.token`. Null es el sitio de siempre.
  final String? tokenPath;

  DaemonClient? _vivo;
  Future<DaemonClient>? _abriendo;

  /// A live client. Concurrent callers share one opening instead of racing to
  /// open several: the daemon only accepts eight connections and the screens
  /// ask at the same time.
  Future<DaemonClient> client() {
    final DaemonClient? actual = _vivo;
    if (actual != null) return Future<DaemonClient>.value(actual);
    return _abriendo ??= _abrir();
  }

  Future<DaemonClient> _abrir() async {
    try {
      final DaemonClient c = await _nuevoCliente();
      _vivo = c;
      return c;
    } finally {
      _abriendo = null;
    }
  }

  /// Throws the current client away. The next [client] opens another.
  Future<void> drop() async {
    final DaemonClient? c = _vivo;
    _vivo = null;
    await c?.close();
  }

  /// A SECOND connection, greeted, that the caller owns and closes.
  ///
  /// # Why a second one is not a shortcut
  ///
  /// Because one connection's server loop is sequential: read, dispatch,
  /// answer. `create_room` holds it for up to ninety seconds, so asking for
  /// the progress down the same connection would queue behind exactly the
  /// operation it wants to watch, and arrive when there is nothing left to
  /// show.
  ///
  /// It is not memoized, unlike [client]: this one is opened for a poll and
  /// closed after. The daemon takes eight, the interface holds one, so a
  /// second is well inside the budget — and closing it is the caller's job
  /// precisely so it cannot pile up.
  Future<DaemonClient> spare() => _nuevoCliente();

  Future<DaemonClient> _nuevoCliente() async {
    final Future<String?> Function()? loadToken = _tokenLoader;
    final String? token = loadToken == null
        ? await readApiToken(path: tokenPath)
        : await loadToken();
    if (token == null) {
      throw const DaemonUnreachable(
        'no está el archivo del token, así que el servicio de Kanpachi no '
        'está corriendo',
        kind: DaemonUnreachableKind.notConnected,
      );
    }
    final DaemonClient c = DaemonClient(
      transport:
          _transportFactory?.call() ?? WindowsPipeTransport(name: _pipeName),
      token: token,
    );
    await c.connect();
    return c;
  }
}
