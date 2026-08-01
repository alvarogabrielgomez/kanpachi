import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// El diálogo modal, dibujado DENTRO de la ventana de la app y no como una
/// ruta aparte.
///
/// Es a propósito: los tres diálogos de Kanpachi confirman algo que va a
/// cambiar el estado de la sala que se ve por detrás — abrir un juego,
/// expulsar a alguien, renovar el código. Dejar la sala visible tras el velo
/// es lo que da el contexto de qué se está confirmando.
class AppModal extends StatelessWidget {
  const AppModal({
    required this.child,
    required this.onDismiss,
    this.width = 430,
    super.key,
  });

  final Widget child;

  /// Clic fuera del cuadro. Siempre existe: un modal sin salida es una
  /// trampa, y ninguno de estos tres es obligatorio.
  final VoidCallback onDismiss;

  final double width;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Positioned.fill(
      child: TweenAnimationBuilder<double>(
        duration: AppMotion.dialog,
        curve: AppMotion.enter,
        tween: Tween<double>(begin: 0, end: 1),
        builder: (BuildContext context, double t, Widget? body) {
          return Opacity(opacity: t, child: body);
        },
        child: Stack(
          children: <Widget>[
            GestureDetector(
              onTap: onDismiss,
              child: Container(color: colors.scrim),
            ),
            Center(
              child: Padding(
                padding: const EdgeInsets.all(AppSpacing.x9l),
                child: ConstrainedBox(
                  constraints: BoxConstraints(maxWidth: width),
                  child: Container(
                    padding: const EdgeInsets.all(AppSpacing.x8l),
                    decoration: BoxDecoration(
                      color: colors.surface,
                      borderRadius: AppRadius.allXxl,
                      boxShadow: <BoxShadow>[
                        BoxShadow(
                          color: colors.shadow,
                          blurRadius: 60,
                          spreadRadius: -26,
                          offset: const Offset(0, 34),
                        ),
                      ],
                    ),
                    child: child,
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// La fila de acciones de un diálogo: cancelar a la izquierda, confirmar a la
/// derecha, siempre en ese orden. Invertirlo entre dos diálogos de la misma
/// app es cómo se consigue que alguien expulse a un amigo por reflejo.
class AppModalActions extends StatelessWidget {
  const AppModalActions({
    required this.confirmLabel,
    required this.onConfirm,
    required this.onCancel,
    this.cancelLabel = 'Cancelar',
    super.key,
  });

  final String confirmLabel;
  final VoidCallback onConfirm;
  final VoidCallback onCancel;
  final String cancelLabel;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisAlignment: MainAxisAlignment.end,
      children: <Widget>[
        AppButton(
          label: cancelLabel,
          onPressed: onCancel,
          variant: AppButtonVariant.ghost,
        ),
        const SizedBox(width: AppSpacing.lg),
        AppButton(
          label: confirmLabel,
          onPressed: onConfirm,
          variant: AppButtonVariant.primaryFlat,
        ),
      ],
    );
  }
}
