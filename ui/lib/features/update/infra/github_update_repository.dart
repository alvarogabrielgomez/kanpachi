import 'dart:convert';
import 'dart:io';

import 'package:kanpachi_ui/core/brand.dart';
import 'package:kanpachi_ui/features/update/domain/repositories/update_repository.dart';
import 'package:kanpachi_ui/core/timing/app_timing.dart';

/// Asks the update channel which version is the latest.
///
/// # Why GitHub and not the registry, which is what it used to do
///
/// It asked the seed, and that made sense while the seed was one compiled host:
/// a machine this client talked to anyway, with the answer already cached for
/// everyone. Since the registry became anybody's, believing it about versions
/// means **a stranger's machine choosing which version you install** — answer an
/// old tag and this window goes quiet forever on a build that is behind.
///
/// The reason for the detour was GitHub's quota: sixty per hour PER IP, shared
/// by everyone behind the same router. That argument was about the AUTOMATIC
/// check, which asked several times per session with nobody asking for it. On
/// demand, a person pressing a button spends one of sixty.
///
/// # Why it swallows everything
///
/// The contract is a nullable answer and never a throw. The caller is a screen
/// showing a line at the bottom of the window; there is nothing it could do with
/// the difference between a socket error and a captive portal answering HTML.
class GithubUpdateRepository implements UpdateRepository {
  const GithubUpdateRepository({this.timeout = kUpdateCheckTimeout});

  /// El plazo de cada paso. Ver [kUpdateCheckTimeout], que es el valor por
  /// omisión y el porqué; entra por parámetro solo para que los tests lo
  /// acorten.
  final Duration timeout;

  @override
  Future<String?> latestVersion() async {
    // The switch is read here, before the socket. A fork that turns it off does
    // not have to find the callers: there is no path left that can leak out.
    if (!Brand.updatesEnabled) return null;

    final HttpClient cliente = HttpClient()..connectionTimeout = timeout;
    try {
      final HttpClientRequest peticion = await cliente
          .getUrl(Uri.parse(Brand.latestApi))
          .timeout(timeout);
      peticion.headers.set(
        HttpHeaders.acceptHeader,
        'application/vnd.github+json',
      );
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
      final Object? tag = crudo['tag_name'];
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
