import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
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
          Row(
            children: <Widget>[
              Expanded(
                child: AppButton(
                  label: 'Cancelar',
                  variant: AppButtonVariant.ghost,
                  height: 48,
                  onPressed: () => shell.go(AppScreen.home),
                ),
              ),
              const SizedBox(width: AppSpacing.lg),
              Expanded(
                flex: 2,
                child: AppButton(
                  label: 'Entrar a la sala',
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

/// La bandeja: la ventana cerrada y la sala viva.
///
/// Existe como pantalla porque es la parte del modelo que más cuesta creer:
/// cerrar la ventana NO cierra la sala. El daemon la sostiene, y los demás
/// siguen jugando. Enseñarlo una vez ahorra la llamada de "se me cerró y se
/// cayó todo".
class TrayScreen extends StatelessWidget {
  const TrayScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final SessionState session = context.watch<SessionCubit>().state;
    final ShellCubit shell = context.read<ShellCubit>();

    // Ocupa la ventana entera: es una simulación del escritorio con la app
    // cerrada, así que recortarla a una altura fija rompería la ilusión que da
    // sentido a la pantalla.
    return ColoredBox(
      color: colors.surfaceSunken,
      child: SizedBox.expand(
        child: Stack(
          children: <Widget>[
            Center(
              child: Padding(
                padding: const EdgeInsets.all(AppSpacing.x10l),
                child: Text(
                  'Cerrar la ventana no cierra la sala: el daemon la sostiene.\n'
                  'Queda el icono en la bandeja.',
                  textAlign: TextAlign.center,
                  style: context.type.body.copyWith(color: colors.textMuted),
                ),
              ),
            ),
            Positioned(
              right: AppSpacing.x3l,
              bottom: 56,
              child: SizedBox(
                width: 250,
                child: AppCard(
                  clip: true,
                  radius: AppRadius.allLg,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: AppSpacing.xxl,
                          vertical: AppSpacing.xl,
                        ),
                        child: Row(
                          children: <Widget>[
                            AppStatusDot(color: colors.ok),
                            const SizedBox(width: 9),
                            Expanded(
                              child: Text(
                                session.room == null
                                    ? 'Sin sala'
                                    : 'En ${session.room!.name} · '
                                        '${session.room!.members.length} personas',
                                style: context.type.label
                                    .copyWith(color: colors.text),
                              ),
                            ),
                          ],
                        ),
                      ),
                      Divider(color: colors.border, height: 1),
                      AppRow(
                        onTap: () => shell.go(
                          session.room == null ? AppScreen.home : AppScreen.room,
                        ),
                        child: Text(
                          'Abrir Kanpachi',
                          style: context.type.label.copyWith(color: colors.text),
                        ),
                      ),
                      Divider(color: colors.border, height: 1),
                      AppRow(
                        onTap: () {
                          context.read<SessionCubit>().leave();
                          shell.go(AppScreen.home);
                        },
                        child: Text(
                          'Salir de la sala',
                          style: context.type.label.copyWith(color: colors.text),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
            Positioned(
              left: 0,
              right: 0,
              bottom: 0,
              child: Container(
                height: 44,
                padding:
                    const EdgeInsets.symmetric(horizontal: AppSpacing.x4l),
                decoration: BoxDecoration(
                  color: colors.surface,
                  border: Border(
                    top: BorderSide(
                      color: colors.border,
                      width: AppStroke.hairline,
                    ),
                  ),
                ),
                child: Row(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: <Widget>[
                    Container(
                      width: 18,
                      height: 18,
                      alignment: Alignment.center,
                      decoration: BoxDecoration(
                        color: colors.accent,
                        borderRadius: AppRadius.allXs,
                      ),
                      child: Text(
                        'k',
                        style: context.type.monoXxs.copyWith(
                          color: colors.accentInk,
                          fontSize: 10,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                    const SizedBox(width: AppSpacing.xxl),
                    Text(
                      '21:14',
                      style:
                          context.type.monoSm.copyWith(color: colors.textMuted),
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
