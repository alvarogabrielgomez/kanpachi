import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_status_dot.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/motion_tokens.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';

/// El tono de un aviso.
enum AppNoticeTone {
  /// Ámbar. Algo que el usuario puede arreglar y que le conviene arreglar:
  /// el firewall apagado, un puerto abierto en el router, una regla que dejó
  /// el propio juego. Nunca rojo — no hay nada roto, hay algo mejorable, y el
  /// rojo pediría una urgencia que no existe.
  warn,

  /// Neutro. Un hecho que cambia lo que se puede hacer pero que no es culpa
  /// de nadie: el host se fue de la sala.
  neutral,

  /// Confirmación de algo que ya se hizo: la regla quedó desactivada.
  done,
}

/// El aviso: punto de color, título y explicación.
///
/// La explicación es obligatoria a propósito. Un aviso que sólo dice "tu
/// firewall está apagado" deja al usuario con el susto y sin salida; el
/// diseño siempre cuenta qué implica y qué hacer, y esta clase hace que no se
/// pueda escribir uno sin eso.
class AppNotice extends StatelessWidget {
  const AppNotice({
    required this.title,
    required this.body,
    this.tone = AppNoticeTone.warn,
    this.actions,
    this.pulse = false,
    super.key,
  });

  /// La variante de una sola línea, sin título: el estado de la red.
  const AppNotice.line({
    required this.body,
    this.tone = AppNoticeTone.warn,
    this.pulse = false,
    super.key,
  })  : title = null,
        actions = null;

  final String? title;

  /// Qué implica y, cuando corresponde, qué hacer.
  final Widget body;

  final AppNoticeTone tone;
  final List<Widget>? actions;

  /// Late cuando el aviso describe algo que está pasando ahora mismo, como una
  /// reconexión en curso.
  final bool pulse;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final (Color background, Color dot) = switch (tone) {
      AppNoticeTone.warn => (colors.warnSurface, colors.warn),
      AppNoticeTone.neutral => (colors.chip, colors.textMuted),
      AppNoticeTone.done => (colors.chip, colors.ok),
    };

    final bool oneLine = title == null && actions == null;

    return Container(
      width: double.infinity,
      padding: EdgeInsets.symmetric(
        horizontal: oneLine ? AppSpacing.x3l : AppSpacing.x3l,
        vertical: oneLine ? AppSpacing.xl : AppSpacing.xxl,
      ),
      decoration: BoxDecoration(
        color: background,
        borderRadius: oneLine ? AppRadius.allLg : AppRadius.allLg,
      ),
      child: Row(
        crossAxisAlignment:
            oneLine ? CrossAxisAlignment.center : CrossAxisAlignment.start,
        children: <Widget>[
          Padding(
            padding: EdgeInsets.only(top: oneLine ? 0 : 5),
            child: AppStatusDot(
              color: dot,
              pulse: pulse,
              pulseDuration: AppMotion.pulseFast,
              square: tone == AppNoticeTone.neutral && title != null,
            ),
          ),
          const SizedBox(width: AppSpacing.xl),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                if (title != null) ...<Widget>[
                  Text(
                    title!,
                    style: context.type.strong.copyWith(color: colors.text),
                  ),
                  const SizedBox(height: 3),
                ],
                DefaultTextStyle(
                  style: context.type.bodySm.copyWith(color: colors.textOnChip),
                  child: body,
                ),
                if (actions != null) ...<Widget>[
                  const SizedBox(height: AppSpacing.xl),
                  Wrap(
                    spacing: AppSpacing.md,
                    runSpacing: AppSpacing.md,
                    children: actions!,
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}
