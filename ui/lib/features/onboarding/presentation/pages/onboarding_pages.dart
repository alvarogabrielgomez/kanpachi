import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_spinner.dart';
import 'package:kanpachi_ui/core/design_system/atoms/kanpachi_wordmark.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_ambient_background.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/widgets/progress_steps.dart';
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
  void _continue() {
    if (!_valid) return;
    context.read<SessionCubit>().setNickname(_controller.text);
    context.read<ShellCubit>().go(AppScreen.home);
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return ScreenCentered(
      child: Column(
        children: <Widget>[
          Text(
            widget.fromOnboarding
                ? '¿Cómo te ven tus panas?'
                : 'Cambia tu nombre',
            textAlign: TextAlign.center,
            style: context.type.display.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.x8l),
          AppField(
            controller: _controller,
            shape: AppFieldShape.hero,
            maxLength: 12,
            autofocus: true,
            hint: 'Alvaro',
            onSubmitted: (_) => _continue(),
          ),
          const SizedBox(height: AppSpacing.lg),
          Text(
            'Hasta 12 letras y números',
            // El rol de nota, el mismo de «Paso 2 de 2»: es una pista, no una
            // etiqueta, y en semi-negrita competía con el campo que explica.
            style: context.type.bodySm.copyWith(
              color: colors.textMuted,
              height: 1,
            ),
          ),
          const SizedBox(height: AppSpacing.x7l),
          const AppExplainer(
            'Es solo para que se reconozcan en la lista. No es una cuenta, no '
            'se verifica, no se manda a ningún servidor.',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: AppSpacing.x8l),
          AppButton(
            label: widget.fromOnboarding ? 'Continuar' : 'Guardar',
            width: 220,
            // Disabled until the name is usable. The daemon would refuse it
            // anyway; refusing here is what lets the refusal be about what
            // they typed.
            onPressed: _valid ? _continue : null,
          ),
          if (widget.fromOnboarding) ...<Widget>[
            const SizedBox(height: AppSpacing.x4l),
            Text(
              'Paso 2 de 2 · solo la primera vez',
              style: context.type.bodySm.copyWith(color: colors.textMuted),
            ),
          ],
        ],
      ),
    );
  }
}

/// Las dos esperas: creando la sala y buscándola.
///
/// Dicen qué está pasando y, sobre todo, qué NO está pasando todavía — que no
/// hay ningún puerto abierto, que el tráfico del juego no pasa por el
/// servidor. Es el momento en que alguien se pregunta qué acaba de autorizar,
/// y contestarlo ahí vale más que en cualquier ayuda.
class ProgressScreen extends StatelessWidget {
  const ProgressScreen({
    required this.title,
    required this.note,
    this.onCancel,
    super.key,
  });

  const ProgressScreen.creating({super.key})
    : title = 'Creando la sala…',
      note =
          'Levantando la red de la sala y generando el código. Todavía no '
          'hay ningún puerto abierto.',
      onCancel = null;

  final String title;
  final String note;
  final VoidCallback? onCancel;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.x10l),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const AppSpinner(),
            const SizedBox(height: AppSpacing.x4l),
            Text(
              title,
              style: context.type.sectionTitle.copyWith(color: colors.text),
            ),
            const SizedBox(height: AppSpacing.x4l),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 360),
              child: Text(
                note,
                textAlign: TextAlign.center,
                style: context.type.body.copyWith(color: colors.textMuted),
              ),
            ),
            if (onCancel != null) ...<Widget>[
              const SizedBox(height: AppSpacing.x4l),
              AppButton(
                label: 'Cancelar',
                variant: AppButtonVariant.ghost,
                onPressed: onCancel,
              ),
            ],
            // What the daemon is doing right now. Debug builds only, and it
            // paints itself away when there is nothing: `state.progress` is
            // never even fetched in release. See [ProgressSteps].
            const _PasosSiEstamosDepurando(),
          ],
        ),
      ),
    );
  }
}

/// The step panel, wired to the session. Debug only.
///
/// Its own class so the wait screen stays free of the cubit in release: the
/// `kDebugMode` check is a compile-time constant, so this whole subtree is
/// tree-shaken out of a release build along with the watch on the session.
class _PasosSiEstamosDepurando extends StatelessWidget {
  const _PasosSiEstamosDepurando();

  @override
  Widget build(BuildContext context) {
    if (!kDebugMode) return const SizedBox.shrink();
    final Progress? p = context.watch<SessionCubit>().state.progress;
    if (p == null || p.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.x6l),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 620),
        child: ProgressSteps(progress: p),
      ),
    );
  }
}
