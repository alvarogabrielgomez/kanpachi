import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/messages/app_message.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
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
/// Existe desde el 2026-08-18: el CLI (`kanpachi last`) y el asistente («Go
/// back to the last room I entered») ya ofrecían esta vuelta, y la ventana no
/// llamaba jamás al método que la trae.
class LastRoomNotice extends StatelessWidget {
  const LastRoomNotice({required this.last, required this.onReturn, super.key});

  final LastRoom last;
  final VoidCallback onReturn;

  @override
  Widget build(BuildContext context) {
    final String cual = last.name.isEmpty
        ? 'con el código ${last.code}'
        : '"${last.name}", con el código ${last.code}';

    return AppMessageNotice(
      message: AppMessage(
        severity: MessageSeverity.neutral,
        title: 'Tu última sala sigue abierta en algún lado',
        body:
            'Estuviste en la sala $cual y no se vuelve sola.\n\n'
            'Volver es entrar de nuevo con el mismo código, con la misma '
            'confirmación de siempre. Si la sala ya no existe, el servidor '
            'lo va a decir.',
      ),
      actions: <Widget>[
        AppButton(label: 'Volver a la sala', onPressed: onReturn),
      ],
    );
  }
}
