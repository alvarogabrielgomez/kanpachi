import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_notice.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/messages/app_message.dart';

/// Pinta un [AppMessage] como aviso.
///
/// Es la otra mitad del catálogo: aquel decide QUÉ se dice, este decide cómo se
/// ve. Separarlos es lo que permite que el copy se pueda afirmar en un test sin
/// montar un `MaterialApp`, y que el mismo texto sirva para un aviso en
/// pantalla, para el icono de la bandeja o para el diagnóstico.
///
/// Es una CLASE y no una función que devuelve `Widget`. Con función, el
/// `setState` de quien lo envuelve rebuildea el aviso entero; con clase, Flutter
/// solo rehace lo que cambió.
class AppMessageNotice extends StatelessWidget {
  const AppMessageNotice({
    required this.message,
    this.titleStyle,
    this.actions,
    this.pulse = false,
    super.key,
  });

  final AppMessage message;

  /// Los avisos de salud de la portada llevan el título un punto por debajo del
  /// de la sala. Ver [AppNotice.titleStyle].
  final TextStyle? titleStyle;

  /// Los botones, cuando el aviso pide decidir algo. El catálogo no los trae:
  /// el texto es del producto y la acción es de la pantalla.
  final List<Widget>? actions;

  final bool pulse;

  @override
  Widget build(BuildContext context) {
    // Un mensaje vacío no se pinta como una caja vacía: no se pinta. Salir de
    // la sala por tu cuenta cae acá, y su texto correcto es ninguno.
    if (message.isEmpty) return const SizedBox.shrink();

    final AppNoticeTone tone = switch (message.severity) {
      MessageSeverity.warn => AppNoticeTone.warn,
      MessageSeverity.neutral => AppNoticeTone.neutral,
      MessageSeverity.done => AppNoticeTone.done,
    };

    final Widget body = _MessageBody(message: message);

    if (message.title == null && actions == null) {
      return AppNotice.line(body: body, tone: tone, pulse: pulse);
    }
    return AppNotice(
      title: message.title,
      titleStyle: titleStyle,
      body: body,
      tone: tone,
      actions: actions,
      pulse: pulse,
    );
  }
}

/// El cuerpo del aviso: la explicación, el tramo que señala y el dato del
/// daemon.
///
/// Clase aparte y no un `Widget _body()` dentro del de arriba, por la regla de
/// la casa y porque el estilo lo hereda del `DefaultTextStyle` que pone
/// [AppNotice]: componerlo acá lo mantiene dentro de ese ámbito.
class _MessageBody extends StatelessWidget {
  const _MessageBody({required this.message});

  final AppMessage message;

  @override
  Widget build(BuildContext context) {
    final String? hint = message.hint;
    final String? detail = message.detail;

    final Widget texto = hint == null
        ? Text(message.body)
        : Text.rich(
            TextSpan(
              children: <InlineSpan>[
                TextSpan(text: message.body),
                // El tramo de acento no lleva gesto a propósito: hoy no hay
                // página de ayuda a la que ir, y un enlace que se pulsa y no
                // hace nada es peor que uno que solo señala dónde mirar.
                TextSpan(
                  text: hint,
                  style: TextStyle(color: context.colors.accent),
                ),
              ],
            ),
          );

    if (detail == null) return texto;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        texto,
        const SizedBox(height: AppSpacing.sm),
        // Lo que aportó el daemon va debajo y más apagado: es el dato concreto,
        // no la explicación. Quien ya entendió el aviso no necesita leerlo, y
        // quien va a copiar el diagnóstico sí.
        Text(
          detail,
          style: context.type.monoXs.copyWith(color: context.colors.textMuted),
        ),
      ],
    );
  }
}
