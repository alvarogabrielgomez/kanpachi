import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// El candado de la arquitectura: `presentation → domain ← data`.
///
/// La regla de dependencias no se ve al leer un archivo suelto; se rompe de a
/// un import por vez y sólo se nota cuando ya es imposible testear el dominio
/// sin levantar media app. Este test la comprueba en cada corrida.
void main() {
  final List<File> archivos = Directory('lib')
      .listSync(recursive: true)
      .whereType<File>()
      .where((File f) => f.path.endsWith('.dart'))
      .where((File f) => !f.path.endsWith('.tailor.dart'))
      .toList(growable: false);

  String ruta(File f) => f.path.replaceAll(r'\', '/');

  List<String> importsDe(File f) => f
      .readAsLinesSync()
      .where((String l) => l.startsWith('import '))
      .toList(growable: false);

  test('core y ioc no conocen ninguna feature', () {
    // Son cross-cutting: los usa todo el mundo. En cuanto uno importa una
    // feature, deja de poder reutilizarse y arrastra media app con él.
    final List<String> culpables = <String>[];
    for (final File f in archivos) {
      final String p = ruta(f);
      if (!p.contains('lib/core/') && !p.contains('lib/ioc/')) continue;
      // El IoC es la excepción por definición: su trabajo es conocer a todos
      // para registrarlos. Por eso está aislado en su propio directorio.
      if (p.contains('lib/ioc/ioc_manager.dart')) continue;
      for (final String i in importsDe(f)) {
        if (i.contains('/features/')) culpables.add('$p → $i');
      }
    }
    expect(culpables, isEmpty, reason: culpables.join('\n'));
  });

  test('el dominio no conoce Flutter ni la implementación', () {
    // Un dominio que importa Flutter deja de poder probarse sin binding, y uno
    // que importa infra invierte la flecha: pasaría a depender de CÓMO se
    // obtienen los datos, que es justo lo que la capa existe para ignorar.
    final List<String> culpables = <String>[];
    for (final File f in archivos) {
      final String p = ruta(f);
      if (!p.contains('/domain/')) continue;
      for (final String i in importsDe(f)) {
        final bool prohibido = i.contains('package:flutter/material.dart') ||
            i.contains('package:flutter_bloc') ||
            i.contains('/infra/') ||
            i.contains('/data/') ||
            i.contains('/presentation/');
        if (prohibido) culpables.add('$p → $i');
      }
    }
    expect(culpables, isEmpty, reason: culpables.join('\n'));
  });

  test('la presentación no llama a infra por la puerta de atrás', () {
    // Las pantallas hablan con el contrato de domain, nunca con la
    // implementación. La excepción es la barra de prototipo, cuyo trabajo es
    // precisamente plantar datos falsos.
    final List<String> culpables = <String>[];
    for (final File f in archivos) {
      final String p = ruta(f);
      if (!p.contains('/presentation/')) continue;
      if (p.endsWith('prototype_dock.dart')) continue;
      for (final String i in importsDe(f)) {
        if (i.contains('/infra/')) culpables.add('$p → $i');
      }
    }
    expect(culpables, isEmpty, reason: culpables.join('\n'));
  });

  test('nadie importa por ruta relativa dentro de lib', () {
    // Un import relativo y otro por package: al MISMO archivo producen dos
    // tipos distintos en runtime, y el error que sale ("X no es X") no apunta
    // a ningún sitio útil.
    final List<String> culpables = <String>[];
    for (final File f in archivos) {
      for (final String i in importsDe(f)) {
        if (i.contains("import '") &&
            !i.contains('package:') &&
            !i.contains('dart:')) {
          culpables.add('${ruta(f)} → $i');
        }
      }
    }
    expect(culpables, isEmpty, reason: culpables.join('\n'));
  });
}
