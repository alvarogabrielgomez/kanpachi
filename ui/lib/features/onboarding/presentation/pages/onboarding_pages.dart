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
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
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

/// Paso 2 de 2: el nombre con el que te ven.
class NicknameScreen extends StatefulWidget {
  const NicknameScreen({super.key});

  @override
  State<NicknameScreen> createState() => _NicknameScreenState();
}

class _NicknameScreenState extends State<NicknameScreen> {
  late final TextEditingController _controller = TextEditingController(
    text: context.read<SessionCubit>().state.nickname,
  );

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _continue() {
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
            '¿Cómo te ven tus panas?',
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
          AppButton(label: 'Continuar', width: 220, onPressed: _continue),
          const SizedBox(height: AppSpacing.x4l),
          Text(
            'Paso 2 de 2 · solo la primera vez',
            style: context.type.bodySm.copyWith(color: colors.textMuted),
          ),
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
          ],
        ),
      ),
    );
  }
}
