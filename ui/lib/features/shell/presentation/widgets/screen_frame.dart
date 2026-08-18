import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
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
  const ScreenCentered({
    required this.child,
    this.maxWidth = 430,
    this.onBack,
    super.key,
  });

  final Widget child;
  final double maxWidth;

  /// Volver, en la esquina de arriba a la izquierda.
  ///
  /// # Por qué la pone el MARCO y no cada pantalla
  ///
  /// Porque son cuatro pantallas centradas y ninguna la tenía: elegir servidor,
  /// escribir su password y cambiarse el nombre se entraba y no se salía, salvo
  /// completando lo que pedían. Puesta a mano en cada una serían cuatro flechas
  /// en cuatro sitios ligeramente distintos, que es lo que ya pasó con otras
  /// cosas de este archivo.
  ///
  /// **Nula donde no hay a dónde volver**, y ese caso existe de verdad: la
  /// bienvenida y el nombre que se pide en el alta son el principio del camino.
  /// Una flecha ahí llevaría a ningún sitio, o peor, a saltarse lo único que la
  /// app necesita para poder hacer algo.
  final VoidCallback? onBack;

  @override
  Widget build(BuildContext context) {
    final Widget centro = Center(
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
    if (onBack == null) return centro;
    return Stack(
      children: <Widget>[
        centro,
        // Encima y no en la columna: el contenido está centrado en la ventana
        // y la flecha va en la esquina, igual que en las pantallas con
        // cabecera. Metida en la columna se movería con el contenido y quedaría
        // a media altura.
        //
        // El margen es el mismo que el de [ScreenBody], para que la flecha caiga
        // donde cae en las otras pantallas y no dos píxeles más allá.
        Padding(
          padding: EdgeInsets.fromLTRB(
            AppSpacing.pageInline,
            context.density.pagePad,
            0,
            0,
          ),
          child: Align(
            alignment: Alignment.topLeft,
            child: AppBackButton(onPressed: onBack!),
          ),
        ),
      ],
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

/// La pantalla de "escribe una cosa y guarda", que en esta app hay tres.
///
/// # Por qué es UNA pantalla y no tres parecidas
///
/// Porque cambiarse el nombre, elegir el servidor y escribir su password son la
/// misma pantalla con otras palabras: un título centrado, un campo grande, una
/// línea que explica o corrige, un recuadro que dice para qué sirve, y un botón.
/// Escritas por separado ya se habían separado: ninguna tenía flecha de volver,
/// dos pintaban el `toString()` de una excepción donde va lo que el daemon dice
/// del texto escrito, y ninguna encendía el estado de espera de su botón aunque
/// el botón sepa hacerlo desde siempre.
///
/// Es la misma regla que [AppEditableName] y que los tamaños de [AppButton]: se
/// reusa el componente y se le pasa el estilo, en vez de copiarlo con otras
/// medidas. Lo único que justifica volver a escribir algo es que no pueda
/// compartirse, como la página de invitación, que es HTML y no Dart.
///
/// # Lo que decide cada pantalla, y lo que decide esta
///
/// Esta decide la forma: dónde va cada cosa, cuánto aire lleva, dónde cae la
/// flecha, y que el botón muestre su rueda mientras la acción corre. Cada
/// pantalla decide QUÉ se escribe y qué hacer con ello, que es lo suyo.
class ScreenPrompt extends StatelessWidget {
  const ScreenPrompt({
    required this.title,
    required this.controller,
    required this.actionLabel,
    required this.onSubmit,
    this.subtitle,
    this.hint,
    this.helper,
    this.error,
    this.explainer,
    this.maxLength,
    this.obscure = false,
    this.enabled = true,
    this.busy = false,
    this.onBack,
    this.beforeAction,
    super.key,
  });

  final String title;

  /// Debajo del título, cuando hace falta decir SOBRE QUÉ se está escribiendo.
  ///
  /// Lo pide la pantalla del password, que enseña ahí el nombre del servidor:
  /// una contraseña que se escribe sin ver a quién se la estás dando es
  /// exactamente lo que este producto no puede pedirle a nadie.
  ///
  /// Widget y no texto porque el estilo es del dato: un nombre de servidor va
  /// en monoespaciada, y esta pantalla no tiene por qué saberlo.
  final Widget? subtitle;

  final TextEditingController controller;

  /// Lo que dice el botón. Cambia entre pantallas porque cambia lo que hace:
  /// «Guardar» deja algo puesto, «Continuar» sigue con lo que se estaba
  /// haciendo.
  final String actionLabel;

  /// Se llama al pulsar y también al pulsar Enter dentro del campo. Las dos
  /// llevan al mismo sitio a propósito: quien acaba de escribir algo pulsa
  /// Enter, y una pantalla que lo ignore obliga a ir a buscar el botón.
  final VoidCallback onSubmit;

  final String? hint;

  /// La línea de debajo del campo cuando todo va bien.
  final String? helper;

  /// La misma línea cuando algo se rechazó. **Gana sobre [helper]**: lo que hay
  /// que leer es lo que salió mal, no lo que se explicaba antes de intentarlo.
  final String? error;

  /// El recuadro que dice para qué sirve esto. Widget y no texto porque uno de
  /// los tres lleva algo más que un párrafo.
  final Widget? explainer;

  final int? maxLength;
  final bool obscure;

  /// Si el botón se puede pulsar. Lo decide cada pantalla con su propia regla
  /// sobre lo escrito, que es lo único que no se puede compartir.
  final bool enabled;

  /// Mientras la acción corre. Enciende la rueda DENTRO del botón, que es donde
  /// [AppButton] ya la sabe pintar.
  final bool busy;

  final VoidCallback? onBack;

  /// Lo que va entre la explicación y el botón, cuando la pantalla tiene algo
  /// que preguntar además de lo que se escribe. Hoy lo pide el alta, que
  /// ofrece ahí dejar la máquina como se recomienda: va pegado al botón porque
  /// es ESE botón el que lo va a aplicar, y separarlo del campo es lo que
  /// impide leerlo como parte del nombre.
  final Widget? beforeAction;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final String? linea = error ?? helper;
    return ScreenCentered(
      onBack: onBack,
      child: Column(
        children: <Widget>[
          Text(
            title,
            textAlign: TextAlign.center,
            style: context.type.display.copyWith(color: colors.text),
          ),
          if (subtitle != null) ...<Widget>[
            const SizedBox(height: AppSpacing.lg),
            subtitle!,
          ],
          const SizedBox(height: AppSpacing.x8l),
          AppField(
            controller: controller,
            shape: AppFieldShape.hero,
            autofocus: true,
            hint: hint,
            maxLength: maxLength,
            obscure: obscure,
            // El borde marca lo que la línea de abajo explica. Los dos salen
            // del mismo dato, así que no pueden discrepar.
            invalid: error != null,
            onSubmitted: (_) => onSubmit(),
          ),
          if (linea != null) ...<Widget>[
            const SizedBox(height: AppSpacing.lg),
            Text(
              linea,
              textAlign: TextAlign.center,
              style: context.type.bodySm.copyWith(
                color: error != null ? colors.danger : colors.textMuted,
              ),
            ),
          ],
          if (explainer != null) ...<Widget>[
            const SizedBox(height: AppSpacing.x7l),
            explainer!,
          ],
          if (beforeAction != null) ...<Widget>[
            const SizedBox(height: AppSpacing.x7l),
            beforeAction!,
          ],
          const SizedBox(height: AppSpacing.x8l),
          AppButton(
            label: actionLabel,
            width: 220,
            busy: busy,
            onPressed: enabled && !busy ? onSubmit : null,
          ),
        ],
      ),
    );
  }
}
