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
          child: Transform.translate(
            offset: Offset(0, 8 * (1 - t)),
            child: body,
          ),
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

/// Dos paneles lado a lado, cada uno con su propio scroll.
///
/// # Por qué no vale el scroll único de [ScreenBody]
///
/// Con un solo scroll para toda la pantalla, la columna más larga arrastra a la
/// otra: bajar para leer el tercer aviso de la izquierda se lleva también la
/// lista de juegos o la de miembros, que es justo lo que se estaba mirando. Y
/// como la altura de la página la fija la columna más alta, un aviso que aparece
/// abajo estira el documento entero y mueve de sitio lo que ya estaba puesto.
///
/// Con un scroll por panel, cada columna crece hacia adentro. La izquierda puede
/// acumular avisos hasta hacerse una lista y recorrerse entera sin que la
/// derecha se mueva un píxel. Es la razón de ser de este widget: **el panel
/// izquierdo es donde viven los mensajes**, y necesita poder ser largo sin
/// costarle la vista al panel derecho.
///
/// # La altura tiene que venir acotada
///
/// Los paneles se reparten el alto disponible, así que este widget NO se puede
/// colgar de un scroll — dentro de uno la altura es infinita y no hay nada que
/// repartir. En la app cuelga del `Expanded` del marco, que siempre la acota.
/// Fallar ahí es preferible a un apaño silencioso: un panel que no scrollea
/// porque alguien lo envolvió en un `SingleChildScrollView` no se nota mirando.
class ScreenPanels extends StatelessWidget {
  const ScreenPanels({
    required this.left,
    required this.right,
    this.header,
    this.gap = AppSpacing.x9l,
    super.key,
  });

  /// El panel de los mensajes y de la acción principal.
  final Widget left;

  final Widget right;

  /// Lo que se queda FIJO por encima de los dos paneles.
  ///
  /// La cabecera de la sala va acá: lleva el nombre, el código y los botones del
  /// host, y perderlos de vista al bajar por los avisos obligaría a subir otra
  /// vez para copiar el enlace.
  final Widget? header;

  /// La separación entre las dos columnas, y también entre ellas cuando la
  /// ventana es tan estrecha que se apilan.
  final double gap;

  /// Por debajo de este ancho de contenido dos columnas no son dos columnas:
  /// son dos tiras. Ahí se apilan y vuelve el scroll único, que es la forma
  /// correcta de leer una sola columna.
  static const double _twoColumnMin = 640;

  @override
  Widget build(BuildContext context) {
    final DensityTokens d = context.density;

    return ScreenEnter(
      // El mismo tope que [ScreenBody], y una sola vez para toda la pantalla:
      // ponerlo por panel daría dos topes de 940 y una ventana maximizada
      // enseñaría dos columnas separadas por medio monitor.
      child: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: AppSpacing.contentMax),
          child: LayoutBuilder(
            builder: (BuildContext context, BoxConstraints c) {
              final bool wide =
                  c.maxWidth - AppSpacing.pageInline * 2 >= _twoColumnMin;

              if (!wide) {
                return SingleChildScrollView(
                  padding: EdgeInsets.fromLTRB(
                    AppSpacing.pageInline,
                    d.pagePad,
                    AppSpacing.pageInline,
                    d.pagePad,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      if (header != null) ...<Widget>[
                        header!,
                        SizedBox(height: gap),
                      ],
                      left,
                      SizedBox(height: gap),
                      right,
                    ],
                  ),
                );
              }

              // El aire de arriba lo paga la cabecera cuando la hay, y los
              // paneles arrancan pegados a ella con el hueco normal entre
              // bloques. Sin cabecera lo pagan ellos.
              final double topOfPanels = header == null ? d.pagePad : d.gap;

              return Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  if (header != null)
                    Padding(
                      padding: EdgeInsets.fromLTRB(
                        AppSpacing.pageInline,
                        d.pagePad,
                        AppSpacing.pageInline,
                        0,
                      ),
                      child: header,
                    ),
                  Expanded(
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: <Widget>[
                        Expanded(
                          child: _Panel(
                            padding: EdgeInsets.fromLTRB(
                              AppSpacing.pageInline,
                              topOfPanels,
                              0,
                              d.pagePad,
                            ),
                            child: left,
                          ),
                        ),
                        SizedBox(width: gap),
                        Expanded(
                          child: _Panel(
                            padding: EdgeInsets.fromLTRB(
                              0,
                              topOfPanels,
                              AppSpacing.pageInline,
                              d.pagePad,
                            ),
                            child: right,
                          ),
                        ),
                      ],
                    ),
                  ),
                ],
              );
            },
          ),
        ),
      ),
    );
  }
}

/// Una columna que se recorre sola.
///
/// Con controlador propio y no el primario: dos scrolls verticales sin
/// controlador se pelean por el `PrimaryScrollController` del marco y Flutter
/// lanza.
///
/// Sin barra, como el resto de la app. Ver `_SinBarra` en `main.dart`: la quita
/// del comportamiento de scroll entero, y un `Scrollbar` puesto a mano acá se la
/// saltaría.
class _Panel extends StatefulWidget {
  const _Panel({required this.child, required this.padding});

  final Widget child;
  final EdgeInsets padding;

  @override
  State<_Panel> createState() => _PanelState();
}

class _PanelState extends State<_Panel> {
  final ScrollController _controller = ScrollController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      controller: _controller,
      padding: widget.padding,
      child: widget.child,
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
    this.noteMaxWidth,
    this.trailing,
    super.key,
  });

  final String title;
  final Widget leading;

  /// La línea que explica de qué va la pantalla. Va indentada bajo el título,
  /// alineada con él y no con la flecha.
  final String? note;

  /// Un tope de ancho para esa línea. Sólo lo pide el alta manual, donde la
  /// nota es larga y estirada a 940 px se lee de un extremo al otro de la
  /// ventana. Las demás notas del diseño no lo llevan.
  final double? noteMaxWidth;

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
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxWidth: noteMaxWidth ?? double.infinity,
              ),
              child: Text(
                note!,
                style: context.type.body.copyWith(color: colors.textMuted),
              ),
            ),
          ),
      ],
    );
  }
}
