import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// Lo que ves cuando abres un enlace `kanpachi://`.
///
/// Esta pantalla es una invariante del proyecto hecha interfaz: nada que
/// llegue de fuera surte efecto sin confirmación dentro. El enlace abre la app
/// y enseña qué recibió; entrar lo decides tú, siempre, sin "recordar esta
/// elección".
class InviteScreen extends StatelessWidget {
  const InviteScreen({required this.code, required this.roomName, super.key});

  final String code;
  final String roomName;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();

    return ScreenCentered(
      maxWidth: 460,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(
            'Te invitaron a una sala de Kanpachi',
            textAlign: TextAlign.center,
            style: context.type.titleLg
                .copyWith(color: colors.text, fontSize: 25, height: 1.25),
          ),
          const SizedBox(height: AppSpacing.x5l),
          AppCard(
            clip: true,
            child: Column(
              children: <Widget>[
                _InviteRow(label: 'Sala', value: roomName),
                Divider(color: colors.border, height: 1),
                _InviteRow(label: 'Código', value: code, mono: true),
                Divider(color: colors.border, height: 1),
                const _InviteRow(
                  label: 'Servidor',
                  value: 'kanpachi.accentio.dev',
                  mono: true,
                ),
              ],
            ),
          ),
          const SizedBox(height: AppSpacing.x4l),
          const AppExplainer(
            'Al entrar, tu equipo se conecta con los demás miembros de esta '
            'sala. Nada de tu PC queda expuesto hasta que se elija un juego.',
          ),
          const SizedBox(height: AppSpacing.x7l),
          // 5:7, que es el 1:1,4 del diseño, y el MISMO alto en los dos. Con
          // 1:2 el «Cancelar» quedaba estrecho de más, y sin alto explícito
          // cada uno medía por su interlínea y salían desnivelados.
          Row(
            children: <Widget>[
              Expanded(
                flex: 5,
                child: AppButton(
                  label: 'Cancelar',
                  variant: AppButtonVariant.ghost,
                  height: 46,
                  textStyle: context.type.label.copyWith(fontSize: 14.5),
                  onPressed: () => shell.go(AppScreen.home),
                ),
              ),
              const SizedBox(width: AppSpacing.lg),
              Expanded(
                flex: 7,
                child: AppButton(
                  label: 'Entrar a la sala',
                  height: 46,
                  onPressed: () {
                    context.read<SessionCubit>().joinRoom(code);
                    shell.go(AppScreen.room);
                  },
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _InviteRow extends StatelessWidget {
  const _InviteRow({
    required this.label,
    required this.value,
    this.mono = false,
  });

  final String label;
  final String value;
  final bool mono;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Padding(
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.x3l,
        vertical: AppSpacing.xl,
      ),
      child: Row(
        children: <Widget>[
          AppKicker(label, small: true),
          const Spacer(),
          Text(
            value,
            style: (mono ? context.type.monoSm : context.type.label)
                .copyWith(color: colors.text),
          ),
        ],
      ),
    );
  }
}

// La bandeja NO vive acá. El diseño la dibuja como una pantalla porque un
// mockup no puede enseñar el menú del icono de la bandeja de Windows de otra
// forma, pero eso es exactamente lo que es: el menú del sistema. Vive en
// `features/shell/infra/windows_tray.dart` y lo mantiene al día `TrayBridge`.
