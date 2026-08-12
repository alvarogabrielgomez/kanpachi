import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_progress_bar.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_spinner.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_ambient_background.dart';
import 'package:kanpachi_ui/core/design_system/tokens/color_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/loading_phrases.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/progress_steps.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// Cuánto lleva hecho la espera, de 0 a 1.
///
/// Sale de los pasos que el daemon ya terminó contra los que suele emitir esa
/// operación, y se topa antes del final: ver `maxLoadingFraction`. Es función
/// suelta y no un método porque la leen dos widgets que no se conocen — la
/// barra y la frase — y tienen que estar de acuerdo.
double _fraccion(Progress? progress, LoadingFlow flow) {
  final int hechos = progress?.steps.length ?? 0;
  if (hechos == 0) return 0;
  return math.min(hechos / expectedSteps(flow), maxLoadingFraction);
}

/// Las cuatro esperas: creando la sala, entrando, saliendo y cerrándola.
///
/// # Qué se ve
///
/// Un rótulo con el anillo que gira, una frase que cambia con lo que va
/// pasando, y una barra que se llena con los pasos que el daemon ya dio. Con el
/// modo verboso encendido, además, el diario de pasos tal cual llega.
///
/// # Por qué frases y no una explicación
///
/// Porque abrir una sala tarda decenas de segundos y una pantalla quieta ese
/// rato se lee como un cuelgue. Las frases salen de `kLoadingTopics`, se elige
/// un tópico al azar por espera, y avanzan con los pasos REALES: moverse
/// significa avanzar.
///
/// **Acá vivía un párrafo que ya no está**, el que decía qué NO estaba pasando
/// todavía — que no había ningún puerto abierto, que el tráfico del juego nunca
/// pasa por el servidor. Se quitó al adoptar este diseño, a sabiendas: era la
/// única vez que la app contestaba esa pregunta en el momento en que alguien se
/// la hace. Si vuelve a hacer falta, va debajo de la barra.
///
/// # Por qué es `const` y no recibe el cancelar por parámetro
///
/// Porque quien la elige —`_CurrentScreen`— se reconstruye con cada muestra de
/// progreso, o sea dos veces por segundo. Un widget `const` sin callbacks cae
/// en la rama de salto total de `Element.updateChild` y no se reconstruye
/// nunca; con una función anónima encima caería siempre en `update()` y
/// arrastraría la pantalla entera. Por eso el botón resuelve él mismo lo suyo,
/// y por eso cada trozo que sí cambia mira la sesión por su cuenta.
class LoadingScreen extends StatelessWidget {
  const LoadingScreen({required this.flow, this.closing = false, super.key});

  final LoadingFlow flow;

  /// Cerrar la sala propia, que comparte las frases de salir y cambia el
  /// rótulo. Ver [loadingKicker].
  final bool closing;

  @override
  Widget build(BuildContext context) {
    return Stack(
      children: <Widget>[
        const _Backdrop(),
        Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpacing.x9l,
              vertical: AppSpacing.x10l,
            ),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 640),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  _Kicker(flow: flow, closing: closing),
                  const SizedBox(height: AppSpacing.md),
                  _Flavor(flow: flow),
                  const SizedBox(height: AppSpacing.x3l),
                  _Bar(flow: flow),
                  const SizedBox(height: AppSpacing.x8l),
                  const _DetailPanel(),
                  const _CancelButton(),
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }
}

/// Las manchas del fondo, con el interruptor del usuario.
///
/// Clase aparte para que sea lo ÚNICO que mira al `ShellCubit`: el resto de la
/// pantalla no tiene por qué reconstruirse porque alguien cambie la densidad o
/// abra un diálogo.
class _Backdrop extends StatelessWidget {
  const _Backdrop();

  @override
  Widget build(BuildContext context) {
    return AppAmbientBackground(
      enabled: context.watch<ShellCubit>().state.ambient,
      layout: AmbientLayout.screen,
    );
  }
}

/// El anillo que gira y el rótulo de qué se está haciendo.
class _Kicker extends StatelessWidget {
  const _Kicker({required this.flow, required this.closing});

  final LoadingFlow flow;
  final bool closing;

  @override
  Widget build(BuildContext context) {
    final ColorTokens colors = context.colors;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        const AppSpinner(size: 14, stroke: 2),
        // 9 y no el escalón de 10 del sistema: es el hueco del diseño, y esta
        // fila es lo único que lo lleva.
        const SizedBox(width: 9),
        Text(
          loadingKicker(flow, closing: closing),
          style: context.type.kickerSm.copyWith(color: colors.textMuted),
        ),
      ],
    );
  }
}

/// La frase temática, y el freno que impide que se atropellen.
///
/// # El tópico
///
/// Se sortea al aparecer y se queda hasta que la espera termina. Volver a
/// sortearlo a mitad rompería lo único que las frases sostienen: que son una
/// sola historia.
///
/// # El freno
///
/// Las frases persiguen el avance real, y el avance real llega a saltos —
/// entrar a una sala de la misma LAN resuelve media operación de golpe. Así que
/// el índice se mueve por un temporizador propio y **como mucho un paso por
/// tic**: aunque la barra salte del 10% al 80%, las frases se encadenan de a
/// una y cada una se alcanza a leer. Nunca retrocede.
///
/// # La entrada NO es un cruce
///
/// La frase que se va desaparece en seco y la que llega entra sola, subiendo 7
/// píxeles mientras aparece. No hay un instante con las dos en pantalla, y eso
/// es literal en el diseño: es UN elemento cuyo texto cambia, y lo que se
/// anima es él. El truco de la maqueta lo delata — alterna entre `kp-fade-a` y
/// `kp-fade-b`, dos animaciones de keyframes IDÉNTICOS, y la única razón para
/// tener dos iguales es que CSS no reinicia una animación cuyo nombre no
/// cambió. O sea: el diseño está pidiendo un reinicio, no una transición entre
/// dos hijos.
///
/// Por eso acá no hay `AnimatedSwitcher`: ese apila la frase vieja y la nueva
/// durante 420 ms y saca la vieja hacia abajo mientras entra la otra. Se veía
/// bien y no era esto.
class _Flavor extends StatefulWidget {
  const _Flavor({required this.flow});

  final LoadingFlow flow;

  @override
  State<_Flavor> createState() => _FlavorState();
}

class _FlavorState extends State<_Flavor> with SingleTickerProviderStateMixin {
  /// Sin semilla fija: lo que se busca es que dos esperas seguidas no cuenten
  /// la misma historia.
  static final math.Random _azar = math.Random();

  /// Cuánto sube la frase mientras aparece. Los 7 px del diseño.
  static const double _rise = 7;

  late LoadingTopic _topic = _sortear();
  int _index = 0;
  Timer? _reloj;

  /// La entrada de la frase que está puesta AHORA.
  ///
  /// Se reinicia con `forward(from: 0)` en cada cambio, que es el equivalente
  /// exacto de lo que la maqueta consigue alternando dos nombres de animación.
  late final AnimationController _entrada = AnimationController(
    vsync: this,
    duration: AppMotion.phraseFade,
  )..forward();

  LoadingTopic _sortear() =>
      kLoadingTopics[_azar.nextInt(kLoadingTopics.length)];

  @override
  void initState() {
    super.initState();
    _reloj = Timer.periodic(AppMotion.phraseDwell, (_) => _avanzar());
  }

  @override
  void didUpdateWidget(_Flavor old) {
    super.didUpdateWidget(old);
    // Otra espera es otra historia: cerrar la sala justo después de abrirla no
    // debería seguir contando la de antes desde la mitad.
    if (old.flow != widget.flow) {
      setState(() {
        _topic = _sortear();
        _index = 0;
      });
      _entrada.forward(from: 0);
    }
  }

  @override
  void dispose() {
    _reloj?.cancel();
    _entrada.dispose();
    super.dispose();
  }

  void _avanzar() {
    if (!mounted) return;
    final List<String> frases = _topic.phrases(widget.flow);
    // Se LEE la sesión, no se la mira: esto se despierta solo, con su propio
    // ritmo, y suscribirse haría que la frase cambiara con cada muestra de
    // progreso — justo lo que el freno existe para impedir.
    final SessionState session = context.read<SessionCubit>().state;
    final double avance = _fraccion(session.progress, widget.flow);
    final int destino = math.min(
      frases.length - 1,
      (avance * frases.length).floor(),
    );
    if (_index >= destino) return;
    setState(() => _index++);
    _entrada.forward(from: 0);
  }

  @override
  Widget build(BuildContext context) {
    final List<String> frases = _topic.phrases(widget.flow);
    final String frase = frases[math.min(_index, frases.length - 1)];

    // Alto fijo: la frase cambia de largo y de alto de línea, y sin esto cada
    // cambio movería la barra y el panel de abajo unos píxeles.
    return SizedBox(
      height: 52,
      child: Center(
        child: AnimatedBuilder(
          animation: _entrada,
          builder: (BuildContext context, Widget? _) {
            final double t = AppMotion.enter.transform(_entrada.value);
            // El desplazamiento envuelve a los puntos también, igual que en la
            // maqueta: ahí la animación está en el div que los CONTIENE, así
            // que la línea entera sube junta.
            return Transform.translate(
              offset: Offset(0, _rise * (1 - t)),
              child: _Line(text: frase, fade: t),
            );
          },
        ),
      ),
    );
  }
}

/// La frase y sus puntos, con el desvanecido de entrada ya resuelto.
///
/// El alfa va en el COLOR y no en un `Opacity` alrededor, la misma regla que en
/// las manchas del fondo: `Opacity` empuja una capa fuera de pantalla, y esto
/// se reconstruye sesenta veces por segundo cada vez que entra una frase.
class _Line extends StatelessWidget {
  const _Line({required this.text, required this.fade});

  final String text;

  /// Cuánto ha entrado la frase, de 0 a 1.
  final double fade;

  @override
  Widget build(BuildContext context) {
    final ColorTokens colors = context.colors;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Flexible(
          child: Text(
            text,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            textAlign: TextAlign.center,
            style: context.type.flavor.copyWith(
              color: colors.text.withValues(alpha: colors.text.a * fade),
            ),
          ),
        ),
        _Dots(fade: fade),
      ],
    );
  }
}

/// Los puntos suspensivos que laten al final de la frase.
///
/// Widget aparte y no parte del texto: laten sin parar, y metidos en el mismo
/// `Text` obligarían a repintar la frase entera sesenta veces por segundo.
class _Dots extends StatefulWidget {
  const _Dots({required this.fade});

  /// Cuánto ha entrado la frase a la que acompañan. Se multiplica con el
  /// latido, igual que en la maqueta: ahí la animación del padre y la del span
  /// componen sus opacidades.
  final double fade;

  @override
  State<_Dots> createState() => _DotsState();
}

class _DotsState extends State<_Dots> with SingleTickerProviderStateMixin {
  /// **Media vuelta, no una entera.**
  ///
  /// El keyframe del diseño va de .25 a 1 y otra vez a .25 DENTRO de sus 1,2 s
  /// (`0%,100%{opacity:.25} 50%{opacity:1}`). Un controlador que repite con
  /// vuelta atrás recorre el ciclo dos veces en su duración, así que con los
  /// 1,2 s puestos enteros latía al doble de lento.
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: AppMotion.dots ~/ 2,
  )..repeat(reverse: true);

  /// Lo más apagados que llegan a estar. Nunca desaparecen del todo: un hueco
  /// que aparece y desaparece al final de la línea la haría bailar.
  static const double _minAlpha = 0.25;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final ColorTokens colors = context.colors;
    return AnimatedBuilder(
      animation: _controller,
      builder: (BuildContext context, Widget? child) {
        final double t = AppMotion.standard.transform(_controller.value);
        final double latido = _minAlpha + (1 - _minAlpha) * t;
        return Text(
          '…',
          style: context.type.flavor.copyWith(
            color: colors.text.withValues(
              alpha: colors.text.a * latido * widget.fade,
            ),
          ),
        );
      },
    );
  }
}

/// La barra, atada a los pasos del daemon.
class _Bar extends StatelessWidget {
  const _Bar({required this.flow});

  final LoadingFlow flow;

  @override
  Widget build(BuildContext context) {
    final Progress? progress = context.watch<SessionCubit>().state.progress;
    return AppProgressBar(value: _fraccion(progress, flow));
  }
}

/// El diario de pasos, para quien pidió que se lo narren.
///
/// Se pinta solo con el modo verboso encendido. El sondeo, en cambio, corre
/// siempre: la barra y las frases salen de ahí. Ver `SessionCubit._watchProgress`.
class _DetailPanel extends StatelessWidget {
  const _DetailPanel();

  /// El alto de la caja, tal cual el diseño. Fijo y no "lo que ocupen los
  /// pasos": con el alto libre, la pantalla entera se movía hacia arriba cada
  /// vez que caía una línea.
  static const double _boxHeight = 196;
  static const double _padH = 17;
  static const double _padV = 15;

  /// Entre la fila DETALLE · MODO VERBOSO y la caja. Del diseño, que no cae en
  /// ningún escalón del sistema.
  static const double _headerGap = 9;

  /// Lo que le queda a la lista por dentro.
  ///
  /// **El borde cuenta**, y medido: un `BoxDecoration` con borde mete al hijo
  /// hacia dentro tanto como el trazo, igual que el padding. Restar sólo el
  /// padding dejaba la lista dos píxeles más alta que su hueco, y se desbordaba
  /// por esos dos.
  static const double _listHeight =
      _boxHeight - _padV * 2 - AppStroke.hairline * 2;

  /// Lo que ocupa el aviso de "se omitieron N pasos" cuando lo hay: una línea
  /// de la mono chica más su separación. La caja es de alto fijo, así que ese
  /// aviso no puede salir de la nada — sale de lo que le queda a la lista.
  static const double _droppedNote = 21;

  @override
  Widget build(BuildContext context) {
    final SessionState session = context.watch<SessionCubit>().state;
    if (!session.verbose) return const SizedBox.shrink();
    final Progress? p = session.progress;
    if (p == null || p.isEmpty) return const SizedBox.shrink();

    final ColorTokens colors = context.colors;
    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.lg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Text(
                'DETALLE',
                style: context.type.kickerXs.copyWith(color: colors.textMuted),
              ),
              const SizedBox(width: AppSpacing.lg),
              Expanded(
                child: ColoredBox(
                  color: colors.border,
                  child: const SizedBox(height: AppStroke.hairline),
                ),
              ),
              const SizedBox(width: AppSpacing.lg),
              Text(
                'MODO VERBOSO',
                style: context.type.kickerXs.copyWith(color: colors.textMuted),
              ),
            ],
          ),
          const SizedBox(height: _headerGap),
          Container(
            height: _boxHeight,
            padding: const EdgeInsets.symmetric(
              horizontal: _padH,
              vertical: _padV,
            ),
            decoration: BoxDecoration(
              color: colors.surfaceSunken,
              border: Border.all(color: colors.border),
              borderRadius: AppRadius.allLg,
            ),
            child: ProgressSteps(
              progress: p,
              // La caja ya acota; el tope de la lista es lo que queda dentro.
              maxHeight: p.dropped > 0
                  ? _listHeight - _droppedNote
                  : _listHeight,
              // La cabecera de la operación la pone la fila de arriba.
              showHeader: false,
              fadePast: true,
            ),
          ),
        ],
      ),
    );
  }
}

/// Cortar la operación.
///
/// # Por qué decide él y no quien lo pinta
///
/// Para que la pantalla siga siendo `const` — ver [LoadingScreen]. Y de paso
/// queda una sola fuente sobre quién se puede cancelar:
/// [SessionState.canCancelWait], que dice que no las dos salidas. **Cortar un
/// desmontaje a la mitad deja exactamente el estado que salir existe para
/// deshacer**: reglas puestas para una sala que ya no está.
class _CancelButton extends StatelessWidget {
  const _CancelButton();

  @override
  Widget build(BuildContext context) {
    if (!context.watch<SessionCubit>().state.canCancelWait) {
      return const SizedBox.shrink();
    }
    // **Sin hueco propio**, y no es un olvido: el aire que lo separa de la
    // barra lo pone la barra, que en el diseño lleva 26 de margen inferior. Los
    // tenía los dos y sumaban 52, o sea el doble del diseño — medido con
    // `scratch/loading_check.dart`, que imprime los huecos reales.
    return AppButton(
      label: 'Cancelar',
      variant: AppButtonVariant.ghost,
      // Las dos órdenes juntas: el daemon deshace lo que alcanzó a hacer y la
      // ventana vuelve a la portada. Navegar sin lo primero dejaría el motor
      // arriba y las reglas puestas por una sala que la app ya no cree estar
      // abriendo.
      onPressed: () {
        context.read<SessionCubit>().cancelPending();
        context.read<ShellCubit>().go(AppScreen.home);
      },
    );
  }
}
