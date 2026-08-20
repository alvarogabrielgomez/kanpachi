import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/last_room.dart';

/// La última sala ajena en la que se estuvo, con el camino de vuelta a mano.
///
/// Aparece solo con la vuelta automática APAGADA: salir a propósito y ser
/// expulsado apagan el interruptor y conservan la sala, que es la promesa de
/// la 0.4.0 — ser expulsado no borra la sala, deja de volverse solo. Mientras
/// el daemon vuelve solo, lo que se pinta es [ReturningNotice] y esto no.
///
/// El botón entra por el MISMO camino que pegar el código: la vista previa y
/// la confirmación de confianza de siempre. Volver es entrar de nuevo, con
/// todo lo que entrar pregunta.
///
/// # Por qué es una fila, y por qué va DENTRO del campo
///
/// Era un [AppMessageNotice] con título, tres renglones y un botón principal,
/// y estaba mal por dónde vivía: la portada es el sitio donde se entra a una
/// sala, y esto empujaba el campo de código hacia abajo para explicar en
/// párrafos lo que el propio botón dice.
///
/// Va en el pie del campo de código ([AppField.footer]) y no suelto debajo:
/// volver es entrar con un código que ya se tiene, así que es del campo y no
/// otra sección de la portada. La caja y el fondo los pinta el pie; esto es la
/// fila y nada más. El texto se recorta con puntos suspensivos porque el
/// nombre de una sala lo escribe otro y puede medir lo que quiera; lo que no
/// se puede perder es el botón.
///
/// Existe desde el 2026-08-18: el CLI (`kanpachi last`) y el asistente («Go
/// back to the last room I entered») ya ofrecían esta vuelta, y la ventana no
/// llamaba jamás al método que la trae.
class LastRoomStrip extends StatelessWidget {
  const LastRoomStrip({
    required this.last,
    required this.onReturn,
    required this.onForget,
    super.key,
  });

  final LastRoom last;
  final VoidCallback onReturn;

  /// La cruz. **Olvida la sala de verdad**, en el disco del daemon: esconder
  /// la fila y nada más la devolvería en el arranque siguiente.
  final VoidCallback onForget;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    // Sin nombre, el código: es lo único que identifica una sala a la que se
    // entró por un enlace pegado y que nadie llegó a bautizar.
    final String cual = last.name.isEmpty ? last.code : last.name;

    return Row(
      spacing: AppSpacing.md,
      children: <Widget>[
        AppIconButton(
          icon: Icons.close,
          tooltip: 'Olvidar esta sala',
          width: 28,
          height: 28,
          iconSize: 13,
          danger: true,
          onPressed: onForget,
        ),
        Expanded(
          child: Text(
            'Volver a $cual',
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: context.type.strong.copyWith(color: colors.text),
          ),
        ),
        AppButton(
          label: 'Volver',
          variant: AppButtonVariant.ghost,
          height: 30,
          horizontalPadding: AppSpacing.x3l,
          onPressed: onReturn,
        ),
      ],
    );
  }
}
