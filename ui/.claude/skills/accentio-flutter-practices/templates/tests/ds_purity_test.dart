// test/core/design_system/ds_purity_test.dart
//
// El CANDADO del design system. `custom_lint` da la DX en el IDE; este test es
// el enforcement DURO en CI, porque `flutter test` ya corre en el pipeline y
// una violación rompe el build sin depender de un step que alguien olvide.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  // El allowlist es MÍNIMO a propósito: esconder deuda en una lista es un no.
  // Sólo entra lo que genuinamente NO es UI de producto.
  const allowlist = <String, String>{
    // 'features/x/window/': 'chrome nativo de la titlebar del OS',
  };

  bool isAllowed(String path) {
    final norm = path.replaceAll('\\', '/');
    return allowlist.keys.any(norm.contains);
  }

  /// Los lookbehind evitan falsos positivos como `AppColors.` (que NO es
  /// `Colors.` de Material) o `AppCard(` (que no es `Card(`).
  final rules = <(String, RegExp)>[
    (
      'Colors.* (usá context.colors.*; Colors.transparent está permitido)',
      RegExp(r'(?<![A-Za-z.])Colors\.(?!transparent\b)'),
    ),
    (
      'Color(0x…) hex hardcodeado (usá un token de context.colors)',
      RegExp(r'(?<![A-Za-z.])Color\(0x'),
    ),
    (
      'GoogleFonts.* (fuente en runtime; usá context.type.*)',
      RegExp(r'(?<![A-Za-z.])GoogleFonts\.'),
    ),
    (
      'fontFamily literal (usá context.type.display/body/mono)',
      RegExp('''fontFamily:\\s*['"]'''),
    ),
    (
      'botón de Material directo (envolvé en AppButton del DS)',
      RegExp(
        r'(?<![A-Za-z.])(FilledButton|ElevatedButton|OutlinedButton|TextButton|IconButton|DropdownButton)\(',
      ),
    ),
    (
      'Card de Material (usá AppCard: level + edge)',
      RegExp(r'(?<![A-Za-z._])Card\('),
    ),
  ];

  test('ningún archivo de UI fuera del DS se salta los tokens', () {
    final violations = <String>[];
    var scanned = 0;

    for (final entity in Directory('lib').listSync(recursive: true)) {
      if (entity is! File || !entity.path.endsWith('.dart')) continue;
      final path = entity.path.replaceAll('\\', '/');

      // El DS es dueño de los primitivos: adentro está permitido.
      if (path.contains('lib/core/design_system/')) continue;
      // Los .g.dart / .freezed.dart los escribe el generador.
      if (path.contains('.g.dart') || path.contains('.freezed.dart')) continue;
      if (isAllowed(path)) continue;

      scanned++;
      final source = entity.readAsStringSync();
      for (final (name, pattern) in rules) {
        for (final match in pattern.allMatches(source)) {
          final line = '\n'.allMatches(source.substring(0, match.start)).length + 1;
          violations.add('$path:$line — $name');
        }
      }
    }

    // Canario: si el barrido se rompe (un cambio de layout de carpetas), el
    // test pasaría por no mirar nada. Verde por vacío es peor que rojo.
    expect(scanned, greaterThan(10), reason: 'el barrido no encontró archivos');

    expect(
      violations,
      isEmpty,
      reason:
          'Violaciones del design system:\n${violations.join('\n')}\n\n'
          'Un valor de marca hardcodeado saca a ese widget del sistema.',
    );
  });
}

// Al escribir un candado así: **mutilá un lado y confirmá que se pone rojo.**
// Un candado que no muerde es peor que ninguno — da confianza falsa.
