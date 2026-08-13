import 'package:flutter/widgets.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/features/session/domain/entities/own_seed.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// El paso previo a abrir una sala, desde donde sea que se pida.
///
/// # Por qué existe como función y no repetido en cada botón
///
/// Porque hay DOS sitios que abren sala —la portada con el botón vacío, y el
/// diálogo de juego cuando se crea con juego elegido—, y el día que uno de los
/// dos se olvidara de preguntar sería el día que alguien abre una sala en el
/// servidor de otro sin verlo. Es el mismo motivo por el que el asistente de la
/// terminal llama a los subcomandos en vez de tener su propio camino.
///
/// Nunca abre nada por su cuenta: o manda a elegir registro, o abre el diálogo
/// de confianza, que es quien llama a `createRoom` si la persona confirma.
Future<void> askToHost(
  BuildContext context, {
  required String suggestedName,
}) async {
  final SessionCubit session = context.read<SessionCubit>();
  final ShellCubit shell = context.read<ShellCubit>();

  // Sin registro configurado no hay nada que confirmar: lo que falta es
  // elegirlo. Un diálogo preguntando «¿confías en?» sobre un nombre vacío sería
  // una pregunta sin dato.
  //
  // La intención se recuerda ANTES de desviar: elegir servidor es un paso de
  // este flujo, no un destino, y al guardar uno la pantalla continúa con la
  // creación en vez de devolver a la portada con la sala sin abrir. La flecha
  // de volver de esa pantalla es la que la olvida.
  final OwnSeed propio = await session.ownSeed();
  if (!context.mounted) return;
  if (!propio.canHost) {
    session.rememberHostIntent(
      name: suggestedName,
      game: session.state.pendingGame,
    );
    shell.go(AppScreen.seed);
    return;
  }
  // Lo que viaja es la SUGERENCIA, no el nombre. El nombre de verdad es el
  // borrador de la sesión, que el diálogo enseña y deja cambiar, y que también
  // se edita en el campo de la portada. Ver [SessionState.roomNameDraft].
  shell.askTrust(
    TrustRequest.hosting(seed: propio.configured, suggestedName: suggestedName),
  );
}
