import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/seed/presentation/widgets/seed_trust_block.dart';
import 'package:kanpachi_ui/features/seed/presentation/widgets/host_trust_chip.dart';
import 'package:kanpachi_ui/features/session/domain/entities/pending_invite.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/ask_trust.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// Lo que ves cuando abres un enlace `kanpachi://`.
///
/// Esta pantalla es una invariante del proyecto hecha interfaz: nada que
/// llegue de fuera surte efecto sin confirmación dentro. El enlace abre la app
/// y enseña qué recibió; entrar lo decides tú, siempre, sin "recordar esta
/// elección".
class InviteScreen extends StatelessWidget {
  const InviteScreen({required this.invite, super.key});

  /// Lo que trajo el enlace, ya resuelto por el daemon.
  final PendingInvite invite;

  /// Si hay una sala a la que se pueda ofrecer entrar.
  ///
  /// Dos motivos la quitan y son distintos: que el enlace no se entienda —lo
  /// mandó una web y puede traer cualquier cosa—, y que el registro AFIRME que
  /// esa sala no existe. Ofrecer el botón igual sería mandar a alguien a
  /// esperar un minuto por algo que ya se sabe que va a fallar.
  bool get _canJoin => invite.understood && !invite.unknown;

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
            // «Se identifica como» y JAMÁS «te invitó»: el apodo no está
            // verificado, así que la pantalla no puede afirmar quién es. Es la
            // misma frase que usa la página, por el mismo motivo.
            invite.hostNick.isEmpty
                ? 'Te invitaron a una sala de Kanpachi'
                : 'Se identifica como ${invite.hostNick}',
            textAlign: TextAlign.center,
            style: context.type.titleLg.copyWith(
              color: colors.text,
              fontSize: 25,
              height: 1.25,
            ),
          ),
          const SizedBox(height: AppSpacing.x5l),
          if (!invite.understood) ...<Widget>[
            // Lo que llegó, tal cual, y sin ofrecer nada. Un enlace que no se
            // entiende viene de una web: enseñarlo es lo único honesto, y
            // enseñarlo entero es lo que permite ver que venía cortado.
            const AppExplainer(
              'Ese enlace no se entiende. Kanpachi espera un código de ocho '
              'caracteres, y esto no lo es. Pídelo de nuevo a quien te invitó, '
              'o pega el código a mano en la portada.',
            ),
            const SizedBox(height: AppSpacing.x4l),
            AppCard(
              clip: true,
              child: _InviteRow(label: 'Llegó', value: invite.link, mono: true),
            ),
          ] else
            AppCard(
              clip: true,
              child: Column(
                children: <Widget>[
                  // El nombre de la sala viene de la tarjeta, que puede no haber
                  // llegado: sin clave en el enlace, con el registro caído, o con
                  // un código dictado por teléfono. Lo que se enseña entonces es
                  // que no se sabe, y no un nombre inventado.
                  _InviteRow(
                    label: 'Sala',
                    value: invite.roomName.isEmpty
                        ? 'Sin nombre todavía'
                        : invite.roomName,
                  ),
                  Divider(color: colors.border, height: 1),
                  _InviteRow(label: 'Código', value: invite.code, mono: true),
                  Divider(color: colors.border, height: 1),
                  _InviteRow(
                    label: 'Servidor',
                    value: invite.seed,
                    mono: true,
                    // La misma etiqueta y en el mismo sitio que en el diálogo,
                    // pegada al servidor. Ver [HostTrustChip].
                    trailing: invite.hasHostTrust
                        ? HostTrustChip(invite: invite)
                        : null,
                  ),
                ],
              ),
            ),
          if (invite.understood) ...<Widget>[
            const SizedBox(height: AppSpacing.x4l),
            if (invite.unknown)
              // El registro contestó que no la conoce. Es un hecho afirmado, no
              // un silencio, y por eso se puede decir así. Acá no va el bloque
              // de confianza: no hay decisión que tomar sobre un servidor si la
              // sala no existe, y ponerlo sería pedir que se confíe en algo
              // para nada.
              const AppExplainer(
                'El servidor no conoce esa sala. Puede que ya se haya cerrado, '
                'o que el código se haya renovado desde que te llegó el enlace.',
              )
            else ...<Widget>[
              // El MISMO bloque que enseña el diálogo de confianza, y no un
              // texto propio: es la misma advertencia sobre la misma clase de
              // máquina, y escribirla dos veces es cómo una acaba diciendo algo
              // distinto de la otra. Ver [SeedTrustBlock].
              const SeedTrustBlock(),
            ],
          ],
          const SizedBox(height: AppSpacing.x7l),
          // 5:7, que es el 1:1,4 del diseño, y el MISMO alto en los dos. Con
          // 1:2 el «Cancelar» quedaba estrecho de más, y sin alto explícito
          // cada uno medía por su interlínea y salían desnivelados.
          Row(
            children: <Widget>[
              Expanded(
                flex: 5,
                child: AppButton(
                  label: _canJoin ? 'Cancelar' : 'Cerrar',
                  variant: AppButtonVariant.ghost,
                  height: 46,
                  textStyle: context.type.label.copyWith(fontSize: 14.5),
                  // Cancelar un enlace devuelve a lo que se estaba haciendo, y
                  // eso casi nunca es la portada: el enlace se pone delante
                  // esté donde esté el usuario, así que el caso normal es un
                  // host dentro de su sala al que le pasan un código. Mandarlo
                  // a la portada era pedirle que eligiera entre su sala y una
                  // pantalla que no pidió.
                  onPressed: () {
                    context.read<SessionCubit>().dismissInvite();
                    shell.back();
                  },
                ),
              ),
              if (_canJoin) ...<Widget>[
                const SizedBox(width: AppSpacing.lg),
                Expanded(
                  flex: 7,
                  child: AppButton(
                    // «Confiar y entrar», que es lo que el botón hace de
                    // verdad: esta pantalla ya no solo enseña a qué sala vas,
                    // enseña a qué SERVIDOR, y entrar es aceptarlo.
                    label: 'Confiar y entrar',
                    height: 46,
                    // Sin navegar a mano. La pantalla de sala aparece cuando el
                    // daemon dice que hay sala, y quien lo vigila es el latido:
                    // ir allá antes enseñaría una sala vacía durante el minuto
                    // que tarda en abrirse, y una sala que no llegó a abrirse
                    // dejaría la pantalla puesta sobre nada.
                    //
                    // **Por la compuerta, y ese era el agujero peor.** Esta
                    // pantalla se enseña ENCIMA de la sala abierta a propósito,
                    // porque el caso normal es un host al que le pasan un
                    // código. Sin preguntar, el daemon rechazaba con «ya estás
                    // en una sala» y no había dónde consentir: ese host no podía
                    // aceptar un enlace nunca.
                    //
                    // Lo que se mira es el desplazamiento de la vista previa, que
                    // el daemon calculó contra ESTA sala. La respuesta general
                    // diría que sí volviendo a esta misma sala, o sea pediría
                    // permiso para abandonar justo lo que se está haciendo.
                    onPressed: () {
                      if (askDisplaceFirst(
                        context,
                        DisplaceIntent.acceptInvite,
                        preview: invite.displaces,
                      )) {
                        return;
                      }
                      unawaited(context.read<SessionCubit>().acceptInvite());
                    },
                  ),
                ),
              ],
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
    this.trailing,
  });

  final String label;
  final String value;
  final bool mono;

  /// Lo que va después del valor, cuando la fila tiene algo que decir sobre
  /// él. Null es el caso normal.
  final Widget? trailing;

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
          // Tracking propio: 1,54 es el correcto para los kickers de 11 px del
          // resto de la app; esta tarjeta es la excepción del diseño.
          AppKicker(label, small: true, tracking: 1.1),
          const Spacer(),
          Text(
            value,
            style: (mono ? context.type.monoMd : context.type.label).copyWith(
              color: colors.text,
            ),
          ),
          if (trailing != null) ...<Widget>[
            const SizedBox(width: AppSpacing.lg),
            trailing!,
          ],
        ],
      ),
    );
  }
}

// La bandeja NO vive acá. El diseño la dibuja como una pantalla porque un
// mockup no puede enseñar el menú del icono de la bandeja de Windows de otra
// forma, pero eso es exactamente lo que es: el menú del sistema. Vive en
// `features/shell/infra/windows_tray.dart` y lo mantiene al día `TrayBridge`.
