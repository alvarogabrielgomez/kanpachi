import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/kanpachi_wordmark.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_ambient_background.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/onboarding/presentation/widgets/recommended_setup.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// Paso 1 de 2. Sólo la primera vez.
///
/// Dice qué es Kanpachi en una frase y no pide nada. Un alta que empieza
/// pidiendo datos antes de explicar para qué son es cómo se pierde a la gente
/// que instaló esto porque un amigo se lo mandó.
class WelcomeScreen extends StatelessWidget {
  const WelcomeScreen({required this.ambient, super.key});

  final bool ambient;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Stack(
      children: <Widget>[
        AppAmbientBackground(
          enabled: ambient,
          intensity: 2.1,
          veilOverSurface: true,
        ),
        ScreenCentered(
          child: Column(
            children: <Widget>[
              const KanpachiWordmark(height: 44),
              const SizedBox(height: AppSpacing.x8l),
              Text(
                'Bienvenido a Kanpachi',
                textAlign: TextAlign.center,
                style: context.type.display.copyWith(color: colors.text),
              ),
              const SizedBox(height: AppSpacing.xl),
              Text(
                'Jugar en LAN con tus panas, sin abrir puertos ni crear '
                'cuentas. Creas una sala, pasas el enlace y eligen el juego '
                'adentro.',
                textAlign: TextAlign.center,
                style: context.type.bodyLg.copyWith(color: colors.textMuted),
              ),
              const SizedBox(height: AppSpacing.x9l),
              AppButton(
                label: 'Siguiente',
                width: 220,
                onPressed: () =>
                    context.read<ShellCubit>().go(AppScreen.nickname),
              ),
              const SizedBox(height: AppSpacing.x4l),
              Text(
                'Paso 1 de 2 · solo la primera vez',
                style: context.type.bodySm.copyWith(color: colors.textMuted),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// El nombre con el que te ven. Paso 2 de 2 del alta, y también la pantalla de
/// cambiarlo más tarde.
class NicknameScreen extends StatefulWidget {
  const NicknameScreen({this.fromOnboarding = true, super.key});

  /// Whether this is the second step of signing up.
  ///
  /// It only changes the copy, and the copy was wrong: the "step 2 of 2" line
  /// was fixed, so somebody changing their name from the account menu was told
  /// they were halfway through a sign-up they finished months ago.
  final bool fromOnboarding;

  @override
  State<NicknameScreen> createState() => _NicknameScreenState();
}

/// Minimum length of a nickname.
///
/// The daemon enforces 2 to 20 (decision 21) and rejects anything else, so
/// letting the button through with one character only moves the refusal
/// somewhere the user cannot connect to what they typed.
const int _minNickname = 2;

class _NicknameScreenState extends State<NicknameScreen> {
  late final TextEditingController _controller = TextEditingController(
    text: context.read<SessionCubit>().state.nickname,
  );

  @override
  void initState() {
    super.initState();
    // Redraws the button as they type. Without it the button stays disabled
    // until something else rebuilds the screen.
    _controller.addListener(_onTyped);
  }

  void _onTyped() => setState(() {});

  /// Whether Continuar also leaves the machine set up as recommended.
  ///
  /// Ticked by default, and it is the ONE place in the product where the
  /// quarantine is answered without the person typing an answer. That is
  /// defensible for exactly one reason: it is written on screen, next to the
  /// button, with what it does one click away, before anything happens. It is
  /// not an automatic path — it is this click, and unticking it leaves the
  /// decision UNTAKEN rather than answering no.
  bool _recomendada = true;

  bool get _valid => _controller.text.trim().length >= _minNickname;

  @override
  void dispose() {
    _controller.removeListener(_onTyped);
    _controller.dispose();
    super.dispose();
  }

  /// **A name is mandatory, and it is not a formality.** The daemon refuses
  /// `create_room` and `join_room` without one, so continuing with an empty
  /// field produces an app that opens and cannot do the only two things it is
  /// for.
  /// Y se ESPERA a que el daemon lo confirme antes de navegar. Guardarlo es lo
  /// único que esta pantalla existe para hacer: seguir con el nombre sin
  /// guardar dejaría el alta hecha en la pantalla y sin hacer en la máquina, y
  /// la portada de después pidiendo el nombre otra vez en el próximo arranque.
  Future<void> _continue() async {
    if (!_valid) return;
    final SessionCubit session = context.read<SessionCubit>();
    if (!await session.setNickname(_controller.text)) return;
    if (!mounted) return;
    // The recommended setup rides the SAME button, and only during sign-up.
    // Changing your name later is changing your name: a screen that also
    // rewrote the machine's firewall because you fixed a typo would be doing
    // something nobody asked for, from a screen whose title says otherwise.
    if (widget.fromOnboarding && _recomendada) {
      unawaited(session.setQuarantine(enabled: true));
    }
    context.read<ShellCubit>().go(AppScreen.home);
  }

  @override
  Widget build(BuildContext context) {
    return ScreenPrompt(
      title: widget.fromOnboarding
          ? '¿Cómo te ven tus panas?'
          : 'Cambia tu nombre',
      controller: _controller,
      hint: 'Alvaro',
      maxLength: 12,
      helper: 'Hasta 12 letras y números',
      explainer: const AppExplainer(
        'Es solo para que se reconozcan en la lista. No es una cuenta, no '
        'se verifica, no se manda a ningún servidor.',
        textAlign: TextAlign.center,
      ),
      actionLabel: widget.fromOnboarding ? 'Continuar' : 'Guardar',
      // Apagado hasta que el nombre sirva. El daemon lo rechazaría igual;
      // rechazarlo acá es lo que permite que el rechazo hable de lo escrito.
      enabled: _valid,
      onSubmit: _continue,
      // Solo en el alta. Fuera de ella esta pantalla guarda un nombre y nada
      // más, así que ni se ofrece ni se menciona.
      beforeAction: widget.fromOnboarding
          ? RecommendedSetupRow(
              value: _recomendada,
              onChanged: (bool v) => setState(() => _recomendada = v),
            )
          : null,
      // **Sin flecha en el alta, y con ella al cambiarlo.** En el alta esto es
      // el principio del camino y no hay a dónde volver; peor, volver sería
      // saltarse lo único que la app necesita para poder hacer algo. Cambiarlo
      // después se entra desde el menú de cuenta, y de ahí sí se vuelve.
      onBack: widget.fromOnboarding ? null : context.read<ShellCubit>().back,
    );
  }
}

// Las cuatro esperas —crear, entrar, salir y cerrar— vivían acá y se mudaron a
// `features/session/presentation/pages/loading_page.dart`. Nunca fueron parte
// del alta: las elige la fase de la sesión, no el paso del alta en que va uno.
