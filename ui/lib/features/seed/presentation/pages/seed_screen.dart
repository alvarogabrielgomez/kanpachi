import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/domain/entities/own_seed.dart';
import 'package:kanpachi_ui/features/session/presentation/daemon_failure_text.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// A qué registro le pide sala esta máquina.
///
/// # Por qué hay que preguntarlo, y por qué solo al que hospeda
///
/// Porque Kanpachi dejó de tener un registro de fábrica. El registro es el
/// punto de encuentro de una sala, lo levanta cualquiera, y viaja dentro de
/// cada código: al ENTRAR sale del código que te pegaron, y al ABRIR no hay
/// código todavía, porque el código es justo lo que el registro emite.
///
/// # Por qué entrar a la sala de alguien no lo configura solo
///
/// Se probó ese camino y es peor de lo que parece: la siguiente sala que abras
/// se hospedaría en el servidor de un desconocido sin que nadie lo decidiera, y
/// el diálogo de confianza te llegaría con ese nombre ya puesto. Lo que sí hace
/// es SUGERIRLO acá, marcado como lo que es, para no obligar a nadie a ir a
/// buscar el nombre a un chat.
class SeedScreen extends StatefulWidget {
  const SeedScreen({super.key});

  @override
  State<SeedScreen> createState() => _SeedScreenState();
}

class _SeedScreenState extends State<SeedScreen> {
  final TextEditingController _controller = TextEditingController();

  /// Lo que había guardado y lo que se puede sugerir. Nulo mientras se lee.
  OwnSeed? _actual;

  /// Lo que dijo el daemon al rechazar. Se enseña PEGADO al campo, y no como
  /// aviso de la portada: el error es de lo que esa persona acaba de escribir.
  String? _error;

  bool _guardando = false;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_alEscribir);
    unawaited(_leer());
  }

  @override
  void dispose() {
    _controller.removeListener(_alEscribir);
    _controller.dispose();
    super.dispose();
  }

  /// Redibuja el botón, y BORRA el error anterior.
  ///
  /// Dejarlo puesto mientras alguien corrige lo que escribió es acusar al texto
  /// nuevo de lo que hizo mal el viejo.
  void _alEscribir() => setState(() => _error = null);

  Future<void> _leer() async {
    try {
      final OwnSeed v = await context.read<SessionCubit>().ownSeed();
      if (!mounted) return;
      setState(() {
        _actual = v;
        // Lo configurado gana a la sugerencia: quien ya eligió uno viene a
        // cambiarlo, y encontrarse el de otro en el campo sería proponerle
        // justo lo que no pidió.
        _controller.text = v.configured.isNotEmpty ? v.configured : v.suggested;
      });
    } on Object catch (e) {
      if (!mounted) return;
      setState(() => _error = daemonFailureText(e));
    }
  }

  bool get _valido => _controller.text.trim().isNotEmpty && !_guardando;

  /// Guarda y sigue: por donde se vino, o con la sala que se estaba abriendo.
  ///
  /// El nombre lo valida el DAEMON, que es quien tiene la misma comprobación
  /// que aplica al registro de un código pegado, **y además comprueba que ese
  /// servidor conteste antes de guardarlo**: el momento de descubrir un nombre
  /// mal escrito o un servidor caído es ahora, mirando el campo, no la próxima
  /// vez que se intente abrir una sala. Acá solo se exige que haya algo
  /// escrito: repetir la regla de un nombre de dominio en Dart daría dos sitios
  /// donde arreglarla y uno de los dos se olvidaría.
  ///
  /// # Cuando se llegó acá abriendo una sala
  ///
  /// Esta pantalla es un paso, no un destino: se vuelve a donde se estaba y se
  /// reabre el diálogo de confianza con el servidor recién puesto, que es
  /// exactamente donde el flujo se había interrumpido. La confianza SÍ se
  /// pregunta, aunque el nombre lo haya escrito la misma persona hace un
  /// segundo: es el diálogo donde además se confirma el nombre de la sala, y es
  /// la única puerta por la que se abre una. Ver [SessionCubit.hasHostIntent].
  Future<void> _guardar() async {
    if (!_valido) return;
    final SessionCubit session = context.read<SessionCubit>();
    final ShellCubit shell = context.read<ShellCubit>();
    setState(() => _guardando = true);
    try {
      final OwnSeed v = await session.ownSeed(seed: _controller.text.trim());
      if (!mounted) return;
      final String? nombre = session.hostIntentName;
      shell.back();
      if (nombre != null && v.configured.isNotEmpty) {
        shell.askTrust(
          TrustRequest.hosting(seed: v.configured, suggestedName: nombre),
        );
      }
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _guardando = false;
        _error = _textoDelFallo(e);
      });
    }
  }

  /// El texto del hueco de error, con el caso propio de esta pantalla.
  ///
  /// El daemon contesta [FailureCode.noRegistry] cuando el servidor no
  /// contestó, y el copy del catálogo habla de crear y entrar a salas, que es
  /// donde ese código nace. Acá el contexto es otro: alguien acaba de escribir
  /// un nombre, y lo que hay que decirle es qué puede tener de malo ESO.
  String _textoDelFallo(Object e) {
    if (e is DaemonError && e.resolved == FailureCode.noRegistry) {
      return 'Ese servidor no contesta. Puede estar caído, o el nombre no ser '
          'el suyo: revísalo, prueba con otro, o inténtalo más tarde.';
    }
    return daemonFailureText(e);
  }

  @override
  Widget build(BuildContext context) {
    final OwnSeed? actual = _actual;
    final bool sugerido =
        actual != null &&
        actual.configured.isEmpty &&
        actual.suggested.isNotEmpty;

    return ScreenPrompt(
      title: '¿En qué servidor abres tus salas?',
      controller: _controller,
      hint: 'kanpachi.ejemplo.com',
      helper: sugerido
          ? 'Este es el servidor de la última sala en la que estuviste. '
                'Cámbialo si no es el tuyo.'
          : 'El nombre del servidor, sin https:// y sin barras',
      error: _error,
      explainer: const AppExplainer(
        'Es el punto de encuentro de tus salas: ve tu IP pública y por él '
        'pasa todo el que entre con tu código. Ponlo solo si confías en '
        'quien lo administra.',
        textAlign: TextAlign.center,
      ),
      actionLabel: 'Guardar',
      enabled: _valido,
      busy: _guardando,
      onSubmit: _guardar,
      // Volver siempre se puede, y hasta ahora no: se entraba acá y solo se
      // salía guardando. A esta pantalla se llega desde Configuración, desde
      // el diálogo de confianza y desde intentar abrir una sala sin servidor,
      // y en los tres hay un sitio del que se vino.
      //
      // Volver ABANDONA: la sala que se estaba abriendo se olvida acá, porque
      // si no, guardar el servidor desde Configuración semanas después abriría
      // una sala que ya nadie estaba abriendo.
      onBack: () {
        context.read<SessionCubit>().dropHostIntent();
        context.read<ShellCubit>().back();
      },
    );
  }
}
