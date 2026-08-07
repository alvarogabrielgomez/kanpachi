import 'dart:convert';
import 'dart:io';

import 'package:kanpachi_ui/features/update/domain/repositories/update_repository.dart';

/// Asks the seed which version is the latest, over `GET /api/version`.
///
/// # Why the seed and not GitHub
///
/// The same reason the download page does it this way, and it is worth writing
/// down here too because this is a second caller and the temptation to "just
/// ask GitHub" comes back. Asking `api.github.com` from every installed copy
/// spends the unauthenticated quota of SIXTY PER HOUR PER IP, which everyone
/// behind the same router shares — a house with four people playing would burn
/// it between them — and it tells GitHub the IP of every machine that has
/// Kanpachi installed and is starting it. The seed asks once an hour, for
/// everyone, and already caches the answer. See `registry/release.go`.
///
/// # Why it swallows everything
///
/// The contract is a nullable answer and never a throw, and that is not
/// laziness: this runs on start and when a room opens or closes, which are the
/// three moments where the user is doing something that matters. A failure to
/// learn about a version is not an event in their day. The window keeps
/// whatever it already knew, and asks again next time.
class SeedUpdateRepository implements UpdateRepository {
  const SeedUpdateRepository({
    this.host = _defaultHost,
    this.timeout = const Duration(seconds: 5),
  });

  /// The seed the download page lives on. It is the same host the invite links
  /// carry, and it is a constant rather than a setting because there is no
  /// screen to change it in: a self-hoster edits this and rebuilds, which is
  /// what self-hosting a client already means.
  static const String _defaultHost = 'kanpachi.accentio.dev';

  final String host;

  /// Short, and applied to each step. This is a background question nobody
  /// asked for; a slow answer is worth less than the socket it holds open.
  final Duration timeout;

  @override
  Future<String?> latestVersion() async {
    final HttpClient cliente = HttpClient()..connectionTimeout = timeout;
    try {
      final HttpClientRequest peticion = await cliente
          .getUrl(Uri.https(host, '/api/version'))
          .timeout(timeout);
      final HttpClientResponse respuesta = await peticion.close().timeout(
        timeout,
      );
      if (respuesta.statusCode != HttpStatus.ok) {
        // Drained even when it is not read: an undrained response keeps the
        // connection out of the pool until the socket dies on its own.
        await respuesta.drain<void>();
        return null;
      }
      final String cuerpo = await respuesta
          .transform(utf8.decoder)
          .join()
          .timeout(timeout);
      final Object? crudo = jsonDecode(cuerpo);
      if (crudo is! Map<String, dynamic>) return null;
      // The field is ABSENT when the seed does not know yet, which is a real
      // state and not a failure: it answers with whatever it last learnt, and
      // right after a restart that is nothing. See `Server.version`.
      final Object? tag = crudo['client'];
      if (tag is! String || tag.trim().isEmpty) return null;
      return tag.trim();
    } on Object catch (_) {
      // Everything: SocketException, TimeoutException, FormatException, and a
      // TLS handshake against a captive portal that answers HTML. They are the
      // same event as far as this window is concerned — there is no version to
      // report — and the caller cannot act on the difference.
      return null;
    } finally {
      cliente.close(force: true);
    }
  }
}
