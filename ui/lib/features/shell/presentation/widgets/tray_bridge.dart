import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
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

  @override
  void initState() {
    super.initState();
    windowManager.addListener(this);
    // Sin esto, la cruz del sistema y Alt+F4 cierran de verdad y se saltan
    // todo lo demás.
    unawaited(windowManager.setPreventClose(true));
    unawaited(
      _bandeja.start(
        status: _estado(context.read<SessionCubit>().state),
        onOpen: () => unawaited(_abrirVentana()),
        onLeaveRoom: _salirDeLaSala,
        onQuit: () => unawaited(_cerrarDeVerdad()),
      ),
    );
  }

  @override
  void dispose() {
    windowManager.removeListener(this);
    unawaited(_bandeja.stop());
    super.dispose();
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
    unawaited(context.read<SessionCubit>().leave());
  }

  Future<void> _cerrarDeVerdad() async {
    await _bandeja.stop();
    await windowManager.setPreventClose(false);
    await windowManager.destroy();
  }

  static TrayStatus _estado(SessionState session) {
    final Room? room = session.room;
    if (room == null) return const TrayStatus.noRoom();
    return TrayStatus(
      line: 'En ${room.name} · ${room.members.length} personas',
      hasRoom: true,
    );
  }

  @override
  Widget build(BuildContext context) {
    // Escucha en vez de leer una vez: el menú dice cuánta gente hay dentro, y
    // eso cambia sin que nadie navegue. `update` ya ignora lo que no cambió.
    unawaited(_bandeja.update(_estado(context.watch<SessionCubit>().state)));
    return widget.child;
  }
}
