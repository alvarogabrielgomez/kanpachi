import 'package:flutter/widgets.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/features/session/domain/entities/displacement.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// La compuerta de la ventana: primero lo que se deja atrás, después la máquina
/// a la que se va a hablar.
///
/// # Por qué es una función y no la regla repetida en cada botón
///
/// Porque hay seis caminos que entran a una sala desde esta ventana, y la regla
/// estaba escrita en uno solo. Los otros cinco iban derechos a abrir o entrar,
/// así que el daemon los rechazaba con «ya estás en una sala» sin que nadie
/// hubiera podido decir que sí: un callejón sin salida. El peor era el enlace
/// `kanpachi://`, que se enseña ENCIMA de la sala abierta a propósito porque el
/// caso normal es un host al que le pasan un código — y ese host nunca podía
/// aceptarlo.
///
/// # Quién decide que hace falta preguntar
///
/// El daemon, y esto solo lo lee. Él sabe si hay sala, de quién es y si hay una
/// vuelta en marcha, y publica la respuesta en el estado y en la vista previa de
/// un código. La ventana la deducía de «hay vuelta», que era una tercera copia
/// de una regla ya contestada y que además no veía los otros dos casos. Ver
/// [Displacement] y `core/domain/displacement.go`.
Displacement? whatDisplaces(BuildContext context, {Displacement? preview}) =>
    preview ?? context.read<SessionCubit>().state.health.displaces;

/// Abre la confirmación que toque para entrar a una sala.
///
/// `preview` es la respuesta contra ESE destino, la que trae la vista previa de
/// un código. Manda sobre la general porque distingue lo que la general no
/// puede: volver a la sala a la que ya se está volviendo no desplaza nada, y sin
/// ella la pantalla pediría permiso para abandonar justo lo que va a hacer.
void askTrustOrDisplace(
  BuildContext context,
  TrustRequest next, {
  Displacement? preview,
}) {
  final ShellCubit shell = context.read<ShellCubit>();
  if (whatDisplaces(context, preview: preview) != null) {
    shell.askDisplace(DisplaceIntent.trust, next: next);
    return;
  }
  shell.askTrust(next);
}

/// La misma compuerta para los dos caminos que NO pasan por la confianza: el
/// enlace `kanpachi://`, que ya es su propia pantalla de confianza, y reabrir la
/// sala propia, que es de esta máquina en el registro de esta máquina.
///
/// Devuelve si hubo que preguntar. Con falso, quien llama sigue derecho: el
/// diálogo no se enseña sobre una pregunta que no tiene objeto.
bool askDisplaceFirst(
  BuildContext context,
  DisplaceIntent then, {
  Displacement? preview,
}) {
  if (whatDisplaces(context, preview: preview) == null) return false;
  context.read<ShellCubit>().askDisplace(then);
  return true;
}
