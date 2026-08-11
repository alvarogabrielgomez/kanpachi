import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/platform/app_log.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/domain/tray_presence.dart';
import 'package:kanpachi_ui/ioc/injector.dart';
import 'package:window_manager/window_manager.dart';

/// Mantiene el icono de la bandeja al día y hace que cerrar la ventana la
/// esconda en vez de matar el proceso.
///
/// Esto es lo que hace verdad la frase que la app repite en tres pantallas:
/// cerrar la ventana NO cierra la sala. Sin esto, la cruz mataría el proceso y
/// con él la partida de todos los que estuvieran dentro.
///
/// Envuelve al marco entero y no vive dentro de una pantalla porque tiene que
/// durar lo que dure la ventana; colgado de una pantalla se desmontaría al
/// navegar y el icono desaparecería de la bandeja a mitad de uso.
class TrayBridge extends StatefulWidget {
  const TrayBridge({required this.child, super.key});

  final Widget child;

  @override
  State<TrayBridge> createState() => _TrayBridgeState();
}

class _TrayBridgeState extends State<TrayBridge> with WindowListener {
  final TrayPresence _bandeja = Injector.instance.get<TrayPresence>();

  /// Si el icono está puesto ahora mismo.
  ///
  /// Hace falta porque la bandeja va y viene con el servicio, y `start` y `stop`
  /// no son idempotentes: pedir dos veces el icono deja dos.
  bool _trayUp = false;

  @override
  void initState() {
    super.initState();
    windowManager.addListener(this);
    // Sin esto, la cruz del sistema y Alt+F4 cierran de verdad y se saltan
    // todo lo demás.
    unawaited(windowManager.setPreventClose(true));
    // El icono NO se pone acá. Se pone en `build`, cuando se sabe si hay
    // servicio detrás. Ver [_syncTray].
  }

  @override
  void dispose() {
    windowManager.removeListener(this);
    unawaited(_bandeja.stop());
    super.dispose();
  }

  /// Pone o quita el icono según haya daemon.
  ///
  /// # Por qué la bandeja depende del servicio
  ///
  /// Porque el icono SIGNIFICA que Kanpachi está funcionando. La invariante que
  /// gobierna el producto entero, escrita en `docs/03`, es que hay bandeja si y
  /// solo si hay daemon: un túnel abierto sin nada en pantalla que lo explique
  /// tiene la forma de un troyano.
  ///
  /// La mitad de esa invariante la sostiene el kernel del otro lado, con el Job
  /// Object que se lleva esta ventana cuando el daemon muere. Esta es la otra
  /// mitad, la del caso raro: alguien abrió el ejecutable a mano sin servicio
  /// detrás. Eso es legítimo y enseña el aviso de que no hay servicio, y lo que
  /// no puede hacer es poner un icono que promete algo que no está.
  void _syncTray(SessionState session) {
    final bool wanted = !session.daemonDown;
    if (wanted == _trayUp) {
      // El caso normal de cada rebuild. Se actualiza el texto y ya.
      if (_trayUp) unawaited(_bandeja.update(_estado(session)));
      return;
    }
    _trayUp = wanted;
    if (wanted) {
      unawaited(
        _bandeja.start(
          status: _estado(session),
          onOpen: () => unawaited(_abrirVentana()),
          onLeaveRoom: _salirDeLaSala,
          onQuit: () => unawaited(_cerrarDeVerdad()),
        ),
      );
      return;
    }
    unawaited(_bandeja.stop());
  }

  /// La cruz esconde. Es media promesa; la otra media es que el icono de la
  /// bandeja siga ahí, y de eso se encarga [TrayPresence].
  @override
  void onWindowClose() => unawaited(windowManager.hide());

  Future<void> _abrirVentana() async {
    // `show()` sola no basta si la ventana quedó minimizada: hay que
    // restaurarla primero y enfocarla después, o reaparece detrás de lo que el
    // usuario tenga delante, que para él es lo mismo que no reaparecer.
    if (await windowManager.isMinimized()) await windowManager.restore();
    await windowManager.show();
    await windowManager.focus();
  }

  void _salirDeLaSala() {
    if (!mounted) return;
    // El menú ya deshabilita la entrada mientras se sale, y esto es la otra
    // mitad: el menú se pinta cruzando a C++, así que entre el clic y el
    // repintado hay una ventana en la que la entrada sigue viva. Salir dos
    // veces no es inofensivo del lado del daemon: la segunda llega con la sala
    // ya a medio desmontar.
    final SessionCubit cubit = context.read<SessionCubit>();
    final SessionPhase fase = cubit.state.phase;
    if (fase == SessionPhase.leaving || fase == SessionPhase.closing) return;
    unawaited(cubit.leave());
  }

  /// "Salir de Kanpachi" del menú de la bandeja.
  ///
  /// **Esta ventana no coordina nada: manda la orden y se apaga.** No controla
  /// ninguna de las cosas que hay que apagar —la sala, las reglas del firewall,
  /// el motor, el adaptador virtual—, así que quien las cierra en orden es el
  /// daemon, que es su dueño.
  ///
  /// Lo más probable es que la línea de `destroy` no llegue a ejecutarse: para
  /// entonces el daemon ya se llevó este proceso con el Job Object. Está igual
  /// porque el otro caso existe: un daemon en modo consola no hospeda la
  /// interfaz y contesta que no puede, y ahí cerrar la ventana es exactamente
  /// lo que el usuario pidió.
  Future<void> _cerrarDeVerdad() async {
    // **Antes de pedirlo, no después.** Lo de abajo probablemente no termine:
    // el daemon se lleva este proceso en cuanto atiende la orden. Anotado acá,
    // la última línea del registro dice que el cierre fue pedido, que es lo que
    // separa una salida limpia de una caída.
    AppLog.info('salir de Kanpachi: se le pide el apagado al daemon');
    if (mounted) await context.read<SessionCubit>().quitKanpachi();
    await _bandeja.stop();
    await windowManager.setPreventClose(false);
    await windowManager.destroy();
  }

  static TrayStatus _estado(SessionState session) {
    final Room? room = session.room;
    if (room == null) return const TrayStatus.noRoom();
    // Salir tarda segundos, y sin decirlo el menú se queda diciendo "En tal
    // sala · 3 personas" mientras se cierran los puertos. Ver
    // [TrayStatus.leaving].
    //
    // Las dos fases dicen cosas distintas a propósito, igual que el botón que
    // las dispara: al host se le cierra la sala PARA TODOS, y decirle
    // "saliendo" mentiría por omisión sobre lo que les pasa a los demás.
    if (session.phase == SessionPhase.leaving) {
      return const TrayStatus(
        line: 'Saliendo de la sala…',
        hasRoom: true,
        leaving: true,
      );
    }
    if (session.phase == SessionPhase.closing) {
      return const TrayStatus(
        line: 'Cerrando la sala…',
        hasRoom: true,
        leaving: true,
      );
    }
    return TrayStatus(
      line: 'En ${room.name} · ${room.members.length} personas',
      hasRoom: true,
    );
  }

  @override
  Widget build(BuildContext context) {
    // Escucha en vez de leer una vez, y por DOS motivos: el menú dice cuánta
    // gente hay dentro, que cambia sin que nadie navegue, y el icono aparece o
    // desaparece con el servicio.
    _syncTray(context.watch<SessionCubit>().state);
    return widget.child;
  }
}
