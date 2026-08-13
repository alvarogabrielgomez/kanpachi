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
    // El velo NO entra en el fundido. Animarlo junto con el cuadro hacía que
    // el fondo se aclarara y se oscureciera con él, y lo que tiene que pasar
    // es que el velo esté puesto y el cuadro aparezca encima.
    return Positioned.fill(
      child: Stack(
        children: <Widget>[
          GestureDetector(
            onTap: onDismiss,
            child: Container(color: colors.scrim),
          ),
          TweenAnimationBuilder<double>(
            duration: AppMotion.dialog,
            curve: AppMotion.enter,
            tween: Tween<double>(begin: 0, end: 1),
            builder: (BuildContext context, double t, Widget? body) {
              return Opacity(
                opacity: t,
                child: Transform.translate(
                  offset: Offset(0, 8 * (1 - t)),
                  child: body,
                ),
              );
            },
            child: Center(
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
                    // Si no cabe a lo alto, se recorre. Un modal que se sale
                    // de la ventana se come sus propios botones, y entonces
                    // no hay forma de confirmar ni de cancelar lo que vino a
                    // preguntar. Pasa de verdad: ventana en el mínimo, o el
                    // texto largo de la regla ajena.
                    child: SingleChildScrollView(child: child),
                  ),
                ),
              ),
            ),
          ),
        ],
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
    this.stretch = false,
    super.key,
  });

  final String confirmLabel;
  final VoidCallback onConfirm;
  final VoidCallback onCancel;
  final String cancelLabel;

  /// Los dos botones OCUPAN el ancho, en proporción 1:1,5.
  ///
  /// El valor por omisión los deja a su ancho natural pegados a la derecha, que
  /// es lo que pide el diseño cuando las etiquetas son cortas. Con etiquetas
  /// largas —«Cancelar» y «Confiar y entrar»— esa fila se sale del modal: no es
  /// que quepan justas, es que su ancho natural no depende del sitio que hay.
  /// Estirándolos, el texto se reparte el ancho disponible y la fila deja de
  /// poder desbordar.
  final bool stretch;

  @override
  Widget build(BuildContext context) {
    // Alto explícito y el MISMO en los dos: el diseño los estira a la altura
    // de la fila, y dejando que cada uno midiera por su padding salían
    // desnivelados — el ghost lleva `label` (13,5 con interlínea 1,3) y el
    // relleno `button` (13,5 con 1), así que la misma caja da dos alturas.
    final AppButton cancelar = AppButton(
      label: cancelLabel,
      onPressed: onCancel,
      variant: AppButtonVariant.ghost,
      height: 39.5,
      horizontalPadding: stretch ? AppSpacing.lg : AppSpacing.x5l,
    );
    final AppButton confirmar = AppButton(
      label: confirmLabel,
      onPressed: onConfirm,
      variant: AppButtonVariant.primaryFlat,
      height: 39.5,
      horizontalPadding: stretch ? AppSpacing.lg : AppSpacing.x6l,
      // `primaryFlat` mapea al CTA de 14,5 px de «Unirse» y «Crear sala»;
      // acá el diseño pide 13,5, así que se pide por sitio y no se toca
      // el arquetipo.
      textStyle: context.type.buttonSm,
    );
    if (!stretch) {
      return Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: <Widget>[
          cancelar,
          const SizedBox(width: AppSpacing.lg),
          confirmar,
        ],
      );
    }
    // 2:3, que es el 1:1,5 del diseño en enteros.
    return Row(
      children: <Widget>[
        Expanded(flex: 2, child: cancelar),
        const SizedBox(width: AppSpacing.md),
        Expanded(flex: 3, child: confirmar),
      ],
    );
  }
}
