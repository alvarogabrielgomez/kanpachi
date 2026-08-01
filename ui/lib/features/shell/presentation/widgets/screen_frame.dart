import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// La entrada de una pantalla: aparece subiendo unos píxeles.
///
/// Es corta y sutil a propósito. Su trabajo no es lucirse, es dar continuidad
/// entre dos pantallas que comparten marco: sin ella, cambiar de pantalla es
/// un corte seco y cuesta un instante entender que sigues en el mismo sitio.
class ScreenEnter extends StatelessWidget {
  const ScreenEnter({required this.child, super.key});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return TweenAnimationBuilder<double>(
      duration: AppMotion.screen,
      curve: AppMotion.enter,
      tween: Tween<double>(begin: 0, end: 1),
      builder: (BuildContext context, double t, Widget? body) {
        return Opacity(
          opacity: t,
          child: Transform.translate(offset: Offset(0, 8 * (1 - t)), child: body),
        );
      },
      child: child,
    );
  }
}

/// El cuerpo de una pantalla con contenido: aire vertical según la densidad y
/// margen horizontal fijo.
class ScreenBody extends StatelessWidget {
  const ScreenBody({required this.child, this.bottom, super.key});

  final Widget child;

  /// Cuando el pie necesita menos aire que la cabecera.
  final double? bottom;

  @override
  Widget build(BuildContext context) {
    final DensityTokens d = context.density;
    // El scroll lo pone la pantalla, no el marco. Con el marco scrolleando,
    // la barra de estado se iría hacia arriba al bajar en una lista larga, y
    // el dato de "servicio activo" tiene que estar siempre visible.
    return ScreenEnter(
      child: SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
          AppSpacing.pageInline,
          d.pagePad,
          AppSpacing.pageInline,
          bottom ?? d.pagePad,
        ),
        // El contenido crece con la ventana hasta un tope y ahí se centra. Sin
        // tope, maximizar en un monitor grande estira las dos columnas hasta
        // renglones que no se leen de corrido y separa el título de la sala de
        // sus botones por medio metro de vacío. Se pone acá, en el marco, y no
        // en cada pantalla: es una decisión de la app, no de once pantallas que
        // acabarían con once topes distintos.
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: AppSpacing.contentMax),
            child: child,
          ),
        ),
      ),
    );
  }
}

/// El cuerpo de una pantalla centrada, sin más contenido que un bloque: la
/// bienvenida, el nombre, las esperas.
class ScreenCentered extends StatelessWidget {
  const ScreenCentered({required this.child, this.maxWidth = 430, super.key});

  final Widget child;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(AppSpacing.x10l),
        child: ScreenEnter(
          child: ConstrainedBox(
            constraints: BoxConstraints(maxWidth: maxWidth),
            child: child,
          ),
        ),
      ),
    );
  }
}

/// La cabecera con flecha de volver y título, común al selector de juego, la
/// biblioteca y el alta manual.
class ScreenHeader extends StatelessWidget {
  const ScreenHeader({
    required this.title,
    required this.leading,
    this.note,
    this.trailing,
    super.key,
  });

  final String title;
  final Widget leading;

  /// La línea que explica de qué va la pantalla. Va indentada bajo el título,
  /// alineada con él y no con la flecha.
  final String? note;

  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            leading,
            const SizedBox(width: AppSpacing.xl),
            Expanded(
              child: Text(
                title,
                style: context.type.titleMd.copyWith(color: colors.text),
              ),
            ),
            ?trailing,
          ],
        ),
        if (note != null)
          Padding(
            padding: const EdgeInsets.only(left: 42, top: AppSpacing.xs),
            child: Text(
              note!,
              style: context.type.body.copyWith(color: colors.textMuted),
            ),
          ),
      ],
    );
  }
}
