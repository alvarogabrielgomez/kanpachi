import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/seed/domain/seed_password.dart';
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

  Future<void> _enviar() async {
    if (!_valido || _enviando) return;
    setState(() => _enviando = true);
    try {
      await context.read<SessionCubit>().seedPassword(_controller.text);
      if (!mounted) return;
      // Atrás y no a la portada: se llegó acá en mitad de abrir una sala, y
      // devolver a quien lo escribió al sitio donde estaba es lo que hace que
      // pueda volver a pulsar el botón que falló.
      context.read<ShellCubit>().back();
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _enviando = false;
        _error = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final String seed = context.select<SessionCubit, String>(
      (SessionCubit c) => c.state.ownSeed,
    );

    return ScreenCentered(
      child: Column(
        children: <Widget>[
          Text(
            'Ese servidor pide contraseña',
            textAlign: TextAlign.center,
            style: context.type.display.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.lg),
          if (seed.isNotEmpty)
            Text(
              seed,
              textAlign: TextAlign.center,
              style: context.type.mono.copyWith(color: colors.textMuted),
            ),
          const SizedBox(height: AppSpacing.x7l),
          AppField(
            controller: _controller,
            shape: AppFieldShape.hero,
            autofocus: true,
            obscure: true,
            maxLength: SeedPassword.maxLength,
            hint: 'La contraseña de ese servidor',
            onSubmitted: (_) => _enviar(),
          ),
          const SizedBox(height: AppSpacing.lg),
          Text(
            _error ?? 'Te la da quien administra el servidor',
            textAlign: TextAlign.center,
            style: context.type.bodySm.copyWith(
              color: _error != null ? colors.danger : colors.textMuted,
            ),
          ),
          const SizedBox(height: AppSpacing.x7l),
          const _WhatItGuards(),
          const SizedBox(height: AppSpacing.x8l),
          AppButton(
            label: 'Continuar',
            width: 220,
            onPressed: _valido && !_enviando ? _enviar : null,
          ),
        ],
      ),
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
