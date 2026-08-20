import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_progress_bar.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_spinner.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/app_message.dart';
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
/// # Por qué NO reusa [AppNotice], habiéndolo hecho antes
///
/// Porque este aviso quiere tres cosas que aquel no hace, y que no le tocan a
/// los demás: un anillo girando en el sitio del punto, el botón a la derecha en
/// vez de debajo, y una caja de dos líneas. Metérselas al aviso común habría
/// cambiado la forma de todos los avisos de la app para servir a uno.
///
/// Lo que sí se comparte es lo que debe compartirse: el copy sigue viniendo de
/// [AppMessages.returning], y los átomos (botón, anillo, punto, barra) son los
/// de siempre. Lo único propio de aquí es la disposición.
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

  @override
  Widget build(BuildContext context) {
    final Progress? p = progress;
    final bool conPasos = p != null && p.running && p.steps.isNotEmpty;
    // El reloj llegó a cero: el texto promete que está pasando AHORA.
    final bool intentando = conPasos || returning.nextIn <= Duration.zero;

    final AppMessage mensaje = AppMessages.returning(
      room: returning.name,
      code: returning.code,
      nextIn: returning.nextIn,
      attempts: returning.attempts,
      seedDown: seedDown,
    );

    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 17, vertical: 15),
      decoration: BoxDecoration(
        color: context.colors.chip,
        borderRadius: AppRadius.allXl,
      ),
      child: Row(
        children: <Widget>[
          _ReturningMark(spinning: intentando),
          const SizedBox(width: AppSpacing.xl),
          Expanded(
            child: _ReturningText(message: mensaje, progress: conPasos ? p : null),
          ),
          const SizedBox(width: AppSpacing.xl),
          AppButton(
            label: 'Salir de la sala',
            variant: AppButtonVariant.ghost,
            onPressed: onLeave,
          ),
        ],
      ),
    );
  }
}

/// Lo que se mueve a la izquierda del texto.
///
/// Anillo mientras el intento corre y punto latiendo mientras corre el reloj.
/// La diferencia importa: el anillo promete actividad, y entre intento e
/// intento no hay ninguna, solo espera.
///
/// El `SizedBox` no sobra y el `Center` tampoco: el hueco llega con la anchura
/// apretada de la fila, y contra una restricción apretada un `SizedBox` no
/// encoge. Sin esto el anillo salía de 22 px de alto por el ancho entero de la
/// tarjeta, o sea una raya cruzando el aviso.
class _ReturningMark extends StatelessWidget {
  const _ReturningMark({required this.spinning});

  final bool spinning;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 22,
      height: 22,
      child: Center(
        child: spinning
            ? const AppSpinner(size: 20, stroke: 2)
            : AppStatusDot(
                color: context.colors.textMuted,
                pulse: true,
                pulseDuration: AppMotion.pulseFast,
              ),
      ),
    );
  }
}

/// El título, la línea de estado y la barra cuando hay pasos que la muevan.
///
/// La barra solo aparece con pasos porque se dibuja con lo que YA ocurrió. Sin
/// ellos no se puede prometer cuánto falta, y esa espera la cubre el anillo de
/// al lado.
class _ReturningText extends StatelessWidget {
  const _ReturningText({required this.message, required this.progress});

  final AppMessage message;

  /// Solo llega cuando hay pasos de verdad; quien la pinta ya lo decidió.
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
    final String linea = '${message.body}${message.hint ?? ''}';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Text(
          message.title ?? '',
          style: context.type.strong.copyWith(color: context.colors.text),
        ),
        const SizedBox(height: 3),
        Text(
          linea,
          style: context.type.bodySm.copyWith(color: context.colors.textOnChip),
        ),
        if (progress != null) ...<Widget>[
          const SizedBox(height: AppSpacing.lg),
          SizedBox(width: 300, child: AppProgressBar(value: _avance)),
        ],
      ],
    );
  }
}
