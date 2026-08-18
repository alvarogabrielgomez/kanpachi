import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';

/// El aviso de la cuarentena sin poner, con su botón al lado.
///
/// Vive fuera del home porque se pinta en DOS pantallas: en el home, donde la
/// lista de salud apila todos los avisos, y en la sala, que es donde el usuario
/// vive mientras juega — que es justo cuando el aviso importa, porque abrir la
/// sala es uno de sus disparadores. Detectado en vivo el 2026-08-18: el aviso
/// subió, el CLI lo enseñó, y la ventana no, porque la sala solo pintaba sus
/// avisos propios.
class QuarantineOffNotice extends StatelessWidget {
  const QuarantineOffNotice({super.key, required this.alert});

  final HealthAlert alert;

  @override
  Widget build(BuildContext context) {
    return AppMessageNotice(
      message: AppMessages.alertFromWire(alert.wire, detail: alert.detail),
      titleStyle: context.type.strongSm,
      actions: const <Widget>[CloseQuarantineButton()],
    );
  }
}

/// El botón del aviso: el camino para aplicarla, al lado del problema y nunca
/// solo el problema. Encender no se confirma, igual que en Configuración:
/// poner protección no necesita permiso.
class CloseQuarantineButton extends StatelessWidget {
  const CloseQuarantineButton({super.key});

  @override
  Widget build(BuildContext context) {
    final bool trabajando = context
        .watch<SessionCubit>()
        .state
        .isTogglingQuarantine;
    return AppButton(
      label: trabajando ? 'Cerrando…' : 'Cerrar esos puertos',
      variant: AppButtonVariant.primaryFlat,
      height: 34,
      horizontalPadding: 15,
      onPressed: trabajando
          ? null
          : () => context.read<SessionCubit>().setQuarantine(enabled: true),
    );
  }
}
