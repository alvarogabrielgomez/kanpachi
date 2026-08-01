import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';

/// El indicador de "estamos en eso".
///
/// Un anillo con el arco de acento sobre el borde apagado, como en el diseño.
/// Se usa cuando la espera no tiene duración conocida — levantar la red de una
/// sala, presentar el equipo con los demás miembros — y por eso no hay barra
/// de progreso: una barra promete un final medible que nadie puede prometer
/// acá.
class AppSpinner extends StatelessWidget {
  const AppSpinner({this.size = 44, this.stroke = 3, super.key});

  final double size;
  final double stroke;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return SizedBox(
      width: size,
      height: size,
      child: CircularProgressIndicator(
        strokeWidth: stroke,
        color: colors.accent,
        backgroundColor: colors.border,
        strokeCap: StrokeCap.round,
      ),
    );
  }
}
