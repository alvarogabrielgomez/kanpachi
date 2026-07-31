// lib/core/design_system/design_system.dart

/// El design system del proyecto.
///
/// Un contrato de tokens, un componente por arquetipo, todas las superficies
/// del codebase compartiéndolos.
///
/// ## Cómo usarlo
///
/// ```dart
/// import 'package:<app>/core/design_system/design_system.dart';
///
/// AppCard(
///   level: SurfaceLevel.card,
///   child: Text('HOY', style: context.type.display(size: 13)),
/// )
/// ```
///
/// ## Reglas
///
/// 1. **Los componentes dependen SÓLO de tokens.** Ni un `Color(0x…)`, ni un
///    `Colors.*`, ni un `TextStyle(fontFamily: …)` fuera de `tokens/`.
/// 2. **Color y tipografía viajan por `context`** (pueden cambiar en runtime y
///    animan con `lerp`). **Spacing, motion y breakpoints son `static const`**
///    — volverlos theme-aware costaría el `const` de todo widget que los lea.
/// 3. **Nada acá adentro puede importar la DB local ni una feature.** Es lo que
///    permite que este barrel se importe desde cualquier superficie, incluidas
///    las que corren sin store abierto. El candado es
///    `test/core/design_system/import_purity_test.dart`.
/// 4. **Un widget por clase, `const` siempre que se pueda.** Nada de
///    `Widget _buildFoo()`.
library;

export 'atoms/app_button.dart';
export 'atoms/app_card.dart';
// export 'atoms/app_avatar.dart';
// export 'atoms/app_chip.dart';
// export 'atoms/app_divider.dart';
// export 'atoms/app_field.dart';
// export 'atoms/app_icons.dart';
// export 'molecules/app_modal.dart';
// export 'molecules/app_rows.dart';
// export 'molecules/app_toast.dart';
// export 'molecules/app_top_bar.dart';
export 'responsive.dart';
export 'theme/app_theme.dart';
export 'tokens/breakpoint_tokens.dart';
export 'tokens/color_tokens.dart';
export 'tokens/context_ext.dart';
export 'tokens/motion_tokens.dart';
export 'tokens/spacing_tokens.dart';
export 'tokens/typography_tokens.dart';
