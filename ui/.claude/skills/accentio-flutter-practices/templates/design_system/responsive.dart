// lib/core/design_system/responsive.dart

import 'package:flutter/foundation.dart';

/// ¿Este modal va como bottom sheet o como diálogo centrado?
///
/// **El criterio es la PLATAFORMA, no el ancho.** Un teléfono grande o un
/// foldable reportan un ancho por encima del breakpoint de "phone" pero siguen
/// siendo touch: la hoja tiene que quedar al alcance del pulgar, abajo. Al
/// revés, una ventana de escritorio angosta sigue queriendo el diálogo
/// centrado. Usar el ancho para esto manda diálogos centrados a teléfonos
/// grandes.
///
/// El **ancho** SÍ decide el **layout interno** del contenido (1 vs 3 paneles,
/// densidad de una hoja). Eso es responsividad de contenido y es ortogonal a
/// esta regla de presentación — no confundir los dos ejes.
///
/// Sin dependencias de material ni de la DB local: importable desde cualquier
/// superficie del codebase.
bool prefersBottomSheet() {
  return switch (defaultTargetPlatform) {
    TargetPlatform.android || TargetPlatform.iOS || TargetPlatform.fuchsia => true,
    TargetPlatform.windows || TargetPlatform.macOS || TargetPlatform.linux => false,
  };
}

// De acá derivan, con un solo `if`: el align (bottomCenter / center), la
// transición (slide / fade), el borde (top-only / all) y la pill de arrastre.
// Un solo helper `showAppModal()` en molecules/ los resuelve todos.
