import 'package:flutter/foundation.dart';

/// Un invite ID con el registro que lo emitió, ya separados.
///
/// Los dos campos van juntos porque **un invite ID solo significa algo en el
/// registro que lo emitió**: el mismo ID en dos registros son dos salas
/// distintas que no se conocen.
@immutable
class ParsedInvite {
  const ParsedInvite({required this.id, required this.seed, this.key = ''});

  /// Los ocho caracteres, ya normalizados y en mayúsculas.
  final String id;

  /// El host del registro. Nunca vacío: sin él esto no se construye.
  final String seed;

  /// La clave con que la PÁGINA descifra la tarjeta de presentación, si venía
  /// en el fragmento. La app no la usa para nada —el nombre de la sala se lo da
  /// el host por el canal de control—, y se conserva para poder volver a armar
  /// el enlace que se comparte.
  final String key;

  /// La forma que se enseña y se copia: `A7K2-M9QX@seed.ejemplo.com`.
  String get display => '${InviteCode.format(id)}@$seed';

  /// La forma que se manda por chat, que abre la página en un navegador.
  String get link => 'https://$seed/$id${key.isEmpty ? '' : '#$key'}';

  @override
  bool operator ==(Object other) =>
      other is ParsedInvite &&
      other.id == id &&
      other.seed == seed &&
      other.key == key;

  @override
  int get hashCode => Object.hash(id, seed, key);
}

/// El invite ID, con las mismas reglas que `core/domain` en Go.
///
/// Está duplicado entre Dart y Go a la fuerza: la app tiene que decidir si
/// habilita "Unirse" antes de hablar con nadie. Si los dos lados se separan,
/// la app acepta códigos que el daemon rechaza y el usuario concluye que la
/// app está rota. Cualquier cambio acá va acompañado del mismo cambio allá.
///
/// # Lo que cambió al desaparecer el registro de fábrica
///
/// Un código pelado de ocho caracteres **ya no vale**. Antes caía al registro
/// compilado, y eso convertía el caso peor en el silencioso: pegar el código de
/// un amigo que hospeda en su propio servidor entraba a OTRA sala con el mismo
/// ID, sin un solo error. Ver `domain.ErrSeedMissing`.
abstract final class InviteCode {
  /// Sin 0, 1, I ni O: no hay pareja ambigua que adivinar cuando alguien dicta
  /// un código en voz alta.
  static const String alphabet = '23456789ABCDEFGHJKLMNPQRSTUVWXYZ';

  static const int length = 8;

  /// Tope de lo que se acepta mirar, espejo de `domain.MaxInputLen`. Va antes
  /// de tocar el contenido.
  static const int maxInput = 512;

  /// Quita separadores y pasa a mayúsculas, igual que `normalizeInviteID`.
  static String normalize(String raw) {
    final StringBuffer out = StringBuffer();
    for (final String c in raw.toUpperCase().split('')) {
      if (c == '-' || c == ' ' || c == '_' || c == '\t') continue;
      out.write(c);
    }
    return out.toString();
  }

  /// La forma canónica con guion: `A7K2-M9QX`.
  ///
  /// El guion es presentación y nada más: se ve, se copia y se dicta con él, y
  /// viaja sin él. Quien lo recibe lo descarta igual, mandarlo obligaría a que
  /// TODOS lo descarten, y basta uno que no lo haga para que la sala "no exista"
  /// sin nada que lo explique.
  static String format(String normalized) => normalized.length <= 4
      ? normalized
      : '${normalized.substring(0, 4)}-${normalized.substring(4)}';

  /// Interpreta lo que sea que hayan pegado, o devuelve nulo.
  ///
  /// Espejo de `domain.ParseRoom`, y con el mismo propósito acotado: decidir si
  /// se habilita el botón y poder NOMBRAR el registro en el diálogo de
  /// confianza. **La frontera de entrada hostil sigue siendo el daemon**, y lo
  /// que se le manda es el texto tal cual lo pegaron, sin normalizar acá.
  ///
  /// Las formas, todas con registro:
  ///
  ///     A7K2M9QX@seed.ejemplo.com
  ///     a7k2-m9qx@seed.ejemplo.com
  ///     kanpachi://A7K2M9QX@seed.ejemplo.com
  ///     seed.ejemplo.com/A7K2M9QX
  ///     https://seed.ejemplo.com/A7K2M9QX
  ///     https://seed.ejemplo.com/A7K2M9QX#clave
  static ParsedInvite? parse(String raw) {
    if (raw.length > maxInput) return null;
    String s = raw.trim();
    if (s.isEmpty) return null;

    // El fragmento se aparta ANTES de mirar la forma, y no dentro de una rama.
    // Recortarlo solo en la forma con barra hacía que
    // `kanpachi://A7K2M9QX#clave`, que es justo lo que produce el botón de la
    // página, se rechazara por llevar un carácter que no está en el alfabeto.
    String key = '';
    final int corte = s.indexOf('#');
    if (corte >= 0) {
      key = s.substring(corte + 1);
      s = s.substring(0, corte);
    }

    bool esquemaPropio = false;
    for (final String esquema in <String>[
      'kanpachi://',
      'https://',
      'http://',
    ]) {
      if (s.length >= esquema.length &&
          s.substring(0, esquema.length).toLowerCase() == esquema) {
        s = s.substring(esquema.length);
        esquemaPropio = esquema == 'kanpachi://';
        break;
      }
    }
    // Windows convierte la autoridad sin ruta `kanpachi://A7K2M9QX#clave` en
    // `kanpachi://A7K2M9QX/#clave` antes de entregársela al manejador. Esa barra
    // no es una ruta: es la ruta vacía canonicalizada. Solo vale para el esquema
    // propio; en las formas HTTP una barra final sí es parte de la ruta.
    if (esquemaPropio && s.endsWith('/')) {
      s = s.substring(0, s.length - 1);
    }

    final bool hayArroba = s.contains('@');
    final bool hayBarra = s.contains('/');
    // Las dos formas con host son excluyentes. Traer las dos marcas a la vez no
    // es ninguna de las formas, así que se descarta en vez de interpretarlo.
    if (hayArroba && hayBarra) return null;

    String idCrudo;
    String host;
    if (hayArroba) {
      final List<String> partes = s.split('@');
      if (partes.length != 2) return null;
      idCrudo = partes[0];
      host = partes[1];
    } else if (hayBarra) {
      final int i = s.indexOf('/');
      host = s.substring(0, i);
      idCrudo = s.substring(i + 1);
      // Una barra de más significa una ruta, y por este canal no entran rutas.
      if (idCrudo.contains('/') ||
          idCrudo.contains('?') ||
          idCrudo.contains('&')) {
        return null;
      }
    } else {
      // Un código pelado. Tiene forma de código y le falta el registro, que es
      // lo que la portada explica en vez de decir que no se entiende.
      return null;
    }

    final String id = normalize(idCrudo);
    if (!_esID(id)) return null;
    if (!_esHost(host)) return null;
    return ParsedInvite(id: id, seed: host.toLowerCase(), key: key);
  }

  /// Si lo escrito ya sirve para entrar. Es la condición del botón.
  static bool isComplete(String raw) => parse(raw) != null;

  /// El registro de lo pegado, o vacío. Para poder nombrarlo antes de hablarle.
  static String seedOf(String raw) => parse(raw)?.seed ?? '';

  static bool _esID(String n) {
    if (n.length != length) return false;
    for (int i = 0; i < n.length; i++) {
      if (!alphabet.contains(n[i])) return false;
    }
    return true;
  }

  /// Espejo acotado de `parseSeedHost`: un NOMBRE, jamás una dirección.
  ///
  /// La regla que importa es que la última etiqueta lleve una letra, y no es
  /// cosmética: es lo que cierra `127.1`, `0x7f.0.0.1` y `169.254.169.254`. Acá
  /// se comprueba para no habilitar un botón que el daemon va a rechazar; quien
  /// decide de verdad, y quien además comprueba lo que resuelva el DNS, es él.
  static bool _esHost(String raw) {
    final String h = raw.trim().toLowerCase();
    if (h.isEmpty || h.length > 253) return false;
    if (h.contains(':') || h.contains('@') || h.contains('/')) return false;
    final List<String> etiquetas = h.split('.');
    if (etiquetas.length < 2) return false;
    for (final String e in etiquetas) {
      if (e.isEmpty || e.length > 63) return false;
      if (e.startsWith('-') || e.endsWith('-')) return false;
      for (int i = 0; i < e.length; i++) {
        final String c = e[i];
        final bool ok =
            (c.codeUnitAt(0) >= 0x61 && c.codeUnitAt(0) <= 0x7A) ||
            (c.codeUnitAt(0) >= 0x30 && c.codeUnitAt(0) <= 0x39) ||
            c == '-';
        if (!ok) return false;
      }
    }
    final String ultima = etiquetas.last;
    for (int i = 0; i < ultima.length; i++) {
      final int c = ultima.codeUnitAt(i);
      if (c >= 0x61 && c <= 0x7A) return true;
    }
    return false;
  }
}
