// test/core/design_system/import_purity_test.dart
//
// El design system tiene que ser importable desde CUALQUIER superficie del
// codebase, incluidas las que corren sin DB abierta (una consola de escritorio,
// un flavor auxiliar, un test puro). Si el DS importa la DB local, esa
// superficie muere en runtime y NADA te avisa en compile time.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  /// Paquetes e imports prohibidos dentro de `core/design_system/`.
  const forbidden = <String>[
    'package:objectbox',
    'package:shared_preferences',
    'package:dio',
    '/features/', // el DS no conoce features; las features conocen el DS
  ];

  test('core/design_system/ no importa DB, red ni features', () {
    final violations = <String>[];
    var scanned = 0;

    final dir = Directory('lib/core/design_system');
    for (final entity in dir.listSync(recursive: true)) {
      if (entity is! File || !entity.path.endsWith('.dart')) continue;
      scanned++;
      final path = entity.path.replaceAll('\\', '/');
      final lines = entity.readAsLinesSync();

      for (var i = 0; i < lines.length; i++) {
        final line = lines[i];
        if (!line.trimLeft().startsWith('import ') &&
            !line.trimLeft().startsWith('export ')) {
          continue;
        }
        for (final needle in forbidden) {
          if (line.contains(needle)) {
            violations.add('$path:${i + 1} — importa $needle');
          }
        }
      }
    }

    expect(scanned, greaterThan(3), reason: 'el barrido no encontró archivos');
    expect(violations, isEmpty, reason: violations.join('\n'));
  });
}
