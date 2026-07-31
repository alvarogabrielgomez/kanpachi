import 'package:flutter/foundation.dart';

/// Reglas de presentación responsive del design system. Sin dependencias de
/// Flutter material ni de ObjectBox → importable desde cualquier flavor,
/// incluido el Seed Console desacoplado.

/// REGLA DEL DESIGN SYSTEM: los popups/modales son **bottom sheets en mobile**
/// (touch) y diálogos centrados en desktop. El criterio es la PLATAFORMA, no
/// el ancho: un teléfono grande o un foldable siguen siendo touch y quieren la
/// hoja anclada abajo (alcanzable con el pulgar), mientras que una ventana de
/// escritorio angosta sigue queriendo el diálogo centrado.
///
/// (El ancho sí decide el LAYOUT interno — p. ej. el manager de 1 vs 3 paneles;
/// eso es responsividad de contenido, ortogonal a esta regla de presentación.)
bool prefersBottomSheet() {
  switch (defaultTargetPlatform) {
    case TargetPlatform.android:
    case TargetPlatform.iOS:
      return true;
    case TargetPlatform.windows:
    case TargetPlatform.macOS:
    case TargetPlatform.linux:
    case TargetPlatform.fuchsia:
      return false;
  }
}
