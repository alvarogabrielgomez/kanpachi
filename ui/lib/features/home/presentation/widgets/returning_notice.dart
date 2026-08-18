import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_progress_bar.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/loading_phrases.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/features/session/domain/entities/progress.dart';
import 'package:kanpachi_ui/features/session/domain/entities/returning.dart';

/// El aviso de que esta máquina está volviendo a una sala en la que estuvo.
///
/// # Por qué en la portada y no en una pantalla propia
///
/// Porque volver NO es estar dentro. No hay túnel, no hay puertos y no hay
/// miembros: hay una intención guardada y un reloj. Llevarlo a la pantalla de la
/// sala mentiría sobre lo primero, y esconder el campo de código quitaría
/// justamente lo que hace falta para cambiar de idea.
///
/// # Por qué reusa el aviso de siempre
///
/// [AppNotice] ya sabe latir y ya sabe llevar acciones, y su propia
/// documentación dice que el pulso es para "algo que está pasando ahora mismo".
/// Un componente nuevo para esto sería un segundo sitio donde arreglar el mismo
/// aviso.
class ReturningNotice extends StatelessWidget {
  const ReturningNotice({
    required this.returning,
    required this.onLeave,
    this.progress,
    this.seedDown = false,
    super.key,
  });

  final Returning returning;

  /// La petición formal de dejar de volver. Es el mismo «salir de la sala» de
  /// siempre: apaga la vuelta y conserva la sala guardada, así que volver a mano
  /// se sigue ofreciendo.
  final VoidCallback onLeave;

  /// Que el registro no conteste es una causa DISTINTA para la misma espera, y
  /// manda sobre el motivo del último intento: quien lee esto tiene que saber
  /// dónde mirar, y no es lo mismo un host dormido que un servidor caído.
  final bool seedDown;

  /// Los pasos del intento EN CURSO, cuando lo hay. Entre intento e intento no
  /// llega ninguno y la barra no se pinta: ahí lo que corre es el reloj, y eso
  /// ya lo dice el texto. Pedido el 2026-08-18, mirando una vuelta real: el
  /// aviso decía que volvía y nada se movía mientras el intento avanzaba de
  /// verdad por sus pasos.
  final Progress? progress;

  /// Cuánto lleva el intento, con la vara de ENTRAR a una sala: volver es
  /// entrar con la credencial guardada, así que pasa por los mismos sitios.
  /// Mismo tope que el loading, para que el final real sea el que la llena.
  double get _avance {
    final int hechos = progress?.steps.length ?? 0;
    if (hechos == 0) return 0;
    return math.min(
      hechos / expectedSteps(LoadingFlow.joining),
      maxLoadingFraction,
    );
  }

  @override
  Widget build(BuildContext context) {
    final Progress? p = progress;
    final bool intentando = p != null && p.running && p.steps.isNotEmpty;
    return AppMessageNotice(
      message: AppMessages.returning(
        room: returning.name,
        code: returning.code,
        nextIn: returning.nextIn,
        attempts: returning.attempts,
        seedDown: seedDown,
      ),
      pulse: true,
      below: intentando
          ? Padding(
              padding: const EdgeInsets.only(top: AppSpacing.x3l),
              child: AppProgressBar(value: _avance),
            )
          : null,
      actions: <Widget>[
        AppButton(
          label: 'Salir de la sala',
          variant: AppButtonVariant.ghost,
          onPressed: onLeave,
        ),
      ],
    );
  }
}
