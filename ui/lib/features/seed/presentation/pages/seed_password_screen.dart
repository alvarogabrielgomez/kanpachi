import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/messages/message_keys.dart';
import 'package:kanpachi_ui/features/seed/domain/seed_password.dart';
import 'package:kanpachi_ui/features/session/domain/daemon_failure.dart';
import 'package:kanpachi_ui/features/session/presentation/daemon_failure_text.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// La contraseña que un servidor pide para poder ABRIR salas en él.
///
/// # Cuándo aparece, y por qué no antes
///
/// Cuando abrir una sala falla porque el servidor la pide. No se pregunta al
/// configurar el servidor ni al arrancar: casi ninguno va a pedir contraseña, y
/// preguntar por si acaso sería cobrarle a todo el mundo el roce de un caso raro.
///
/// El mismo momento cubre las tres formas de llegar acá: nunca se puso ninguna,
/// la que había caducó, y quien administra el servidor la cambió. Son una sola
/// porque lo que hay que hacer es idéntico, y porque el servidor se niega a
/// decir cuál de las tres fue.
///
/// # Entrar a una sala nunca pasa por acá
///
/// Y eso no es una omisión: es la decisión del producto. Al invitado no se le
/// pide nada en ningún servidor.
class SeedPasswordScreen extends StatefulWidget {
  const SeedPasswordScreen({super.key});

  @override
  State<SeedPasswordScreen> createState() => _SeedPasswordScreenState();
}

class _SeedPasswordScreenState extends State<SeedPasswordScreen> {
  final TextEditingController _controller = TextEditingController();

  /// Lo que dijo el daemon al rechazar, pegado al campo. Nunca la contraseña.
  String? _error;

  bool _enviando = false;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_alEscribir);
  }

  @override
  void dispose() {
    _controller.removeListener(_alEscribir);
    // El controlador se muere con la pantalla y con él lo tecleado. Es todo lo
    // que la interfaz llega a saber de la contraseña: no se guarda en el cubit,
    // no se guarda en disco y no se vuelve a pedir hasta que haga falta.
    _controller.dispose();
    super.dispose();
  }

  void _alEscribir() => setState(() => _error = null);

  bool get _valido => SeedPassword.isValid(_controller.text);

  /// Entrega la contraseña y SIGUE con la sala que se estaba abriendo.
  ///
  /// # Por qué continúa sola, y por qué no vuelve a preguntar la confianza
  ///
  /// Porque esta pantalla es un paso de abrir una sala, no un destino. Se
  /// llegó acá porque el servidor pidió credencial en mitad de una creación
  /// que YA pasó por el diálogo de confianza: volver a la portada obligaba a
  /// pulsar «Crear» otra vez, a confirmar otra vez el mismo servidor, y a
  /// saber que había que hacerlo. Con el token guardado no falta nada más.
  ///
  /// Sin intención pendiente se vuelve por donde se vino, que es lo correcto
  /// para las otras dos puertas: renovar el código, y reabrir la sala.
  Future<void> _enviar() async {
    if (!_valido || _enviando) return;
    final SessionCubit session = context.read<SessionCubit>();
    final ShellCubit shell = context.read<ShellCubit>();
    setState(() => _enviando = true);
    try {
      await session.seedPassword(_controller.text);
      if (!mounted) return;
      shell.back();
      if (session.hasHostIntent) await session.resumeHostIntent();
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _enviando = false;
        _error = _textoDelFallo(e);
      });
    }
  }

  /// El texto pegado al campo, con el caso propio de esta pantalla.
  ///
  /// Una contraseña rechazada llega como [FailureCode.unauthorized], y el copy
  /// del catálogo para ese código habla del saludo con el daemon, que no es
  /// esto. Acá el único significado posible es que esa contraseña no es la de
  /// ese servidor.
  ///
  /// **No se distingue de una que caducó, y es a propósito**: el registro se
  /// niega a decir cuál de las dos fue, porque distinguirlas solo le regalaría
  /// información a quien esté probando contraseñas. Lo que hay que hacer es lo
  /// mismo en los dos casos, que es escribir la buena.
  String _textoDelFallo(Object e) {
    if (e is DaemonError &&
        (e.resolved == FailureCode.unauthorized ||
            e.resolved == FailureCode.seedPassword)) {
      return 'Esa contraseña no es la de este servidor. Pídesela a quien lo '
          'administra y vuelve a escribirla.';
    }
    return daemonFailureText(e);
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final String seed = context.select<SessionCubit, String>(
      (SessionCubit c) => c.state.ownSeed,
    );

    return ScreenPrompt(
      title: 'Ese servidor pide contraseña',
      subtitle: seed.isEmpty
          ? null
          : Text(
              seed,
              textAlign: TextAlign.center,
              style: context.type.mono.copyWith(color: colors.textMuted),
            ),
      controller: _controller,
      hint: 'La contraseña de ese servidor',
      maxLength: SeedPassword.maxLength,
      obscure: true,
      helper: 'Te la da quien administra el servidor',
      error: _error,
      explainer: const _WhatItGuards(),
      actionLabel: 'Continuar',
      enabled: _valido,
      busy: _enviando,
      onSubmit: _enviar,
      // Volver deja el password sin escribir y devuelve a donde se estaba, que
      // es abrir una sala. Sin flecha, la única salida era escribirlo.
      //
      // Y ABANDONA la creación: quien vuelve atrás decidió no abrirla, así que
      // la intención no puede sobrevivirle esperando a resucitar sola.
      onBack: () {
        context.read<SessionCubit>().dropHostIntent();
        context.read<ShellCubit>().back();
      },
    );
  }
}

/// Qué cierra esa contraseña, dicho sin prometer de más.
///
/// La frase importa: la contraseña le cierra la puerta a desconocidos de
/// internet, y **no** a quien ya se sentó en esta máquina. Decirlo al revés
/// sería vender una protección que el producto no da.
class _WhatItGuards extends StatelessWidget {
  const _WhatItGuards();

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Text(
      'Es la contraseña de quien administra ese servidor, y sirve para abrir '
      'salas en él. Entrar a una sala nunca la pide.',
      textAlign: TextAlign.center,
      style: context.type.bodySm.copyWith(color: colors.textMuted, height: 1.5),
    );
  }
}
