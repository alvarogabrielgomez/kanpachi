import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/messages/app_message.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/features/session/domain/entities/saved_room.dart';

/// La sala propia que quedó del arranque anterior y NO volvió sola.
///
/// # Por qué es un aviso y ya no un diálogo que salta
///
/// Porque el modelo cambió y el diálogo se quedó describiendo el viejo. La sala
/// se reabre **sola** en cada arranque del daemon, así que no hay nada que
/// preguntar: no hay una decisión pendiente, hay una reapertura corriendo. Y como
/// tarda cerca de un minuto, el diálogo aparecía ENCIMA de ella, con sus dos
/// botones equivocados a la vez — «reabrir» daba «ya estás en una sala» y
/// «cerrarla» borraba el fichero con la reapertura en vuelo.
///
/// Lo que sí sigue haciendo falta es una salida cuando la reapertura falla, y eso
/// es lo que esto es: un aviso, en la columna de siempre, sin robar el foco.
/// Quien lo ve es porque su sala no volvió.
class SavedRoomNotice extends StatelessWidget {
  const SavedRoomNotice({
    required this.pending,
    required this.onReopen,
    required this.onDiscard,
    super.key,
  });

  final SavedRoom pending;
  final VoidCallback onReopen;
  final VoidCallback onDiscard;

  @override
  Widget build(BuildContext context) {
    final String cual = pending.name.isEmpty
        ? 'con el código ${pending.displayCode}'
        : '"${pending.name}", con el código ${pending.displayCode}';

    return AppMessageNotice(
      message: AppMessage(
        severity: MessageSeverity.warn,
        title: 'Tu sala no volvió a abrirse',
        body:
            'La sala $cual quedó abierta del arranque anterior y Kanpachi no '
            'pudo reponerla solo.\n\n'
            'Si la reabres vuelve con el mismo código y el mismo juego, así que '
            'los enlaces que ya repartiste siguen valiendo.',
      ),
      actions: <Widget>[
        AppButton(label: 'Reabrir la sala', onPressed: onReopen),
        AppButton(
          label: 'Cerrarla',
          variant: AppButtonVariant.ghost,
          onPressed: onDiscard,
        ),
      ],
    );
  }
}
