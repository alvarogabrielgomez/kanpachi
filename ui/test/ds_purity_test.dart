import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// El candado del design system: ningún color ni ninguna tipografía fuera de
/// los tokens.
///
/// Un `Color(0xFF…)` metido dentro de un componente lo saca del sistema: deja
/// de responder al tema y nadie se entera hasta que alguien lo ve en pantalla,
/// normalmente en modo oscuro y normalmente delante de otra persona. Este test
/// lo encuentra antes.
void main() {
  final Directory lib = Directory('lib');

  /// Dónde SÍ pueden vivir los literales, que es el único sitio donde un color
  /// significa algo por sí mismo.
  bool esTokens(String path) =>
      path.replaceAll(r'\', '/').contains('lib/core/design_system/tokens/');

  bool esGenerado(String path) => path.endsWith('.tailor.dart');

  List<File> archivosDart() => lib
      .listSync(recursive: true)
      .whereType<File>()
      .where((File f) => f.path.endsWith('.dart'))
      .where((File f) => !esGenerado(f.path))
      .toList(growable: false);

  /// Quita comentarios antes de buscar. Sin esto, explicar en un comentario
  /// por qué NO se usa `Colors.red` hace fallar al test que comprueba que no
  /// se usa, y la única salida sería dejar de documentar el motivo.
  String sinComentarios(String fuente) => fuente
      .split('\n')
      .where((String l) => !l.trimLeft().startsWith('//'))
      .join('\n');

  test('ningún color literal fuera de los tokens', () {
    final RegExp literal = RegExp(r'Color\(0x|Colors\.');
    final List<String> culpables = <String>[];

    for (final File f in archivosDart()) {
      if (esTokens(f.path)) continue;
      final String fuente = sinComentarios(f.readAsStringSync());
      // Colors.transparent es la ausencia de color, no un color: no tiene
      // equivalente semántico y no cambia con el tema.
      final String limpio = fuente.replaceAll('Colors.transparent', '');
      if (literal.hasMatch(limpio)) {
        culpables.add(f.path);
      }
    }

    expect(
      culpables,
      isEmpty,
      reason: 'Estos archivos traen un color a mano en vez de pedirlo por '
          'context.colors. Un color hardcodeado deja de responder al tema:\n'
          '${culpables.join('\n')}',
    );
  });

  test('ninguna familia tipográfica fuera de los tokens', () {
    final List<String> culpables = <String>[];

    for (final File f in archivosDart()) {
      if (esTokens(f.path)) continue;
      if (sinComentarios(f.readAsStringSync()).contains('fontFamily:')) {
        culpables.add(f.path);
      }
    }

    expect(
      culpables,
      isEmpty,
      reason: 'Estos archivos fijan una familia a mano en vez de usar un rol '
          'de context.type:\n${culpables.join('\n')}',
    );
  });

  test('ningún widget se construye con un método que devuelva Widget', () {
    // La regla dura del proyecto. Un helper method cae siempre en la rama
    // update() de Element.updateChild: pierde el salto total que habilita un
    // const, y el setState del padre reconstruye el subtree entero.
    final RegExp helper = RegExp(r'^\s*Widget\s+_\w+\s*\(');
    final List<String> culpables = <String>[];

    for (final File f in archivosDart()) {
      final List<String> lineas = f.readAsStringSync().split('\n');
      for (int i = 0; i < lineas.length; i++) {
        if (helper.hasMatch(lineas[i])) {
          culpables.add('${f.path}:${i + 1}');
        }
      }
    }

    expect(
      culpables,
      isEmpty,
      reason: 'Un widget es una CLASE, no un método. Extrae una clase y ponle '
          'const:\n${culpables.join('\n')}',
    );
  });
}
