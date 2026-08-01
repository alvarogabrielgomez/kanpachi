import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';
import 'package:kanpachi_ui/features/session/infra/fake_session_repository.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// La barra de prototipo: saltar a cualquier pantalla y a cualquier estado.
///
/// Sólo se monta en debug. Existe porque la mitad de los estados de esta app
/// son caros o imposibles de provocar a mano — que el host se vaya, que la red
/// se degrade, que el juego haya dejado una regla en el firewall — y sin una
/// forma de plantarlos, esos caminos se diseñan a ciegas y se descubren rotos
/// en producción.
///
/// Los colores están fijados a mano a propósito, y es la única excepción a la
/// regla de que todo sale de tokens: esta barra NO es parte del producto, y
/// tematizarla haría que se camuflara justo cuando su trabajo es no
/// confundirse con la app.
class PrototypeDock extends StatelessWidget {
  const PrototypeDock({super.key});

  static const Color _bg = Color(0xFF1C1A17);
  static const Color _chip = Color(0xFF2A2724);
  static const Color _ink = Color(0xFFC9C2B8);
  static const Color _dim = Color(0xFF7D766C);
  static const Color _on = Color(0xFFE0703F);
  static const Color _onInk = Color(0xFF201E1B);

  @override
  Widget build(BuildContext context) {
    final ShellCubit shell = context.read<ShellCubit>();
    final ShellState state = context.watch<ShellCubit>().state;
    final SessionCubit session = context.read<SessionCubit>();
    final Room? room = context.watch<SessionCubit>().state.room;

    return Padding(
      padding: const EdgeInsets.fromLTRB(
        AppSpacing.x3l,
        0,
        AppSpacing.x3l,
        AppSpacing.x3l,
      ),
      child: Center(
        child: Container(
          padding: const EdgeInsets.all(AppSpacing.sm),
          decoration: BoxDecoration(
            color: _bg,
            borderRadius: AppRadius.allXxl,
          ),
          child: Wrap(
            spacing: 5,
            runSpacing: 5,
            alignment: WrapAlignment.center,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: AppSpacing.md),
                child: Text(
                  'PROTOTIPO',
                  style: TextStyle(
                    color: _dim,
                    fontSize: 11,
                    fontWeight: FontWeight.w600,
                    letterSpacing: 1.54,
                    fontFamily: 'Consolas',
                  ),
                ),
              ),
              _DockButton(
                label: 'Bienvenida',
                on: state.screen == AppScreen.welcome,
                onTap: () => shell.go(AppScreen.welcome),
              ),
              _DockButton(
                label: 'Nombre',
                on: state.screen == AppScreen.nickname,
                onTap: () => shell.go(AppScreen.nickname),
              ),
              _DockButton(
                label: 'Sin sala',
                on: state.screen == AppScreen.home,
                onTap: () {
                  session.leave();
                  shell.go(AppScreen.home);
                },
              ),
              _DockButton(
                label: 'Elegir juego',
                on: state.screen == AppScreen.gamePicker,
                onTap: () => shell.openGamePicker(fromRoom: room != null),
              ),
              _DockButton(
                label: 'Biblioteca',
                on: state.screen == AppScreen.catalog,
                onTap: () => shell.go(AppScreen.catalog),
              ),
              _DockButton(
                label: 'Agregar juego',
                on: state.screen == AppScreen.manualGame,
                onTap: () => shell.go(AppScreen.manualGame),
              ),
              _DockButton(
                label: 'Sala',
                on: state.screen == AppScreen.room && room?.selfIsHost == false,
                onTap: () => _plantRoom(session, shell, host: false),
              ),
              _DockButton(
                label: 'Sala · host',
                on: state.screen == AppScreen.room && room?.selfIsHost == true,
                onTap: () => _plantRoom(session, shell, host: true),
              ),
              _DockButton(
                label: 'Enlace',
                on: state.screen == AppScreen.invite,
                onTap: () => shell.go(AppScreen.invite),
              ),
              _DockButton(
                label: 'Bandeja',
                on: state.screen == AppScreen.tray,
                onTap: () => shell.go(AppScreen.tray),
              ),
              const _Separator(),
              _DockButton(
                label: switch (room?.network) {
                  NetworkHealth.degraded => 'Red: inestable',
                  NetworkHealth.reconnecting => 'Red: reconectando',
                  _ => 'Red: directa',
                },
                onTap: room == null
                    ? null
                    : () => session.debugReplaceRoom(
                          room.copyWith(network: _nextHealth(room.network)),
                        ),
              ),
              _DockButton(
                label: room?.hostLeft ?? false ? 'Host: fuera' : 'Host: presente',
                onTap: room == null
                    ? null
                    : () => session.debugReplaceRoom(
                          room.copyWith(hostLeft: !room.hostLeft),
                        ),
              ),
              _DockButton(
                label: switch (state.themeMode) {
                  ThemeMode.system => 'Tema: auto',
                  ThemeMode.light => 'Tema: claro',
                  ThemeMode.dark => 'Tema: oscuro',
                },
                onTap: shell.cycleTheme,
              ),
              _DockButton(
                label: 'Densidad: ${state.density.label}',
                onTap: shell.cycleDensity,
              ),
              _DockButton(
                label: state.ambient ? 'Fondo: sí' : 'Fondo: no',
                onTap: () => shell.setAmbient(enabled: !state.ambient),
              ),
              _DockButton(
                label: state.showHealthAlerts ? 'Avisos: sí' : 'Avisos: no',
                onTap: () =>
                    shell.setHealthAlerts(enabled: !state.showHealthAlerts),
              ),
            ],
          ),
        ),
      ),
    );
  }

  static NetworkHealth _nextHealth(NetworkHealth current) => switch (current) {
        NetworkHealth.ok => NetworkHealth.degraded,
        NetworkHealth.degraded => NetworkHealth.reconnecting,
        NetworkHealth.reconnecting => NetworkHealth.ok,
      };

  /// Planta una sala completa sin pasar por la espera. La espera es real y
  /// hay que poder verla, pero no cuando lo que se quiere revisar es la sala.
  static Future<void> _plantRoom(
    SessionCubit session,
    ShellCubit shell, {
    required bool host,
  }) async {
    final FakeSessionRepository fake = FakeSessionRepository();
    final Room room =
        host ? await fake.createRoom(name: 'La Guarida') : await fake.joinRoom('A7K2-M9QX');
    session.debugReplaceRoom(room);
    shell.go(AppScreen.room);
  }
}

class _DockButton extends StatelessWidget {
  const _DockButton({required this.label, required this.onTap, this.on = false});

  final String label;
  final VoidCallback? onTap;
  final bool on;

  @override
  Widget build(BuildContext context) {
    final bool enabled = onTap != null;
    return MouseRegion(
      cursor: enabled ? SystemMouseCursors.click : SystemMouseCursors.basic,
      child: GestureDetector(
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.xl,
            vertical: AppSpacing.md,
          ),
          decoration: BoxDecoration(
            color: on ? PrototypeDock._on : PrototypeDock._chip,
            borderRadius: AppRadius.pill,
          ),
          child: Text(
            label,
            style: TextStyle(
              color: on
                  ? PrototypeDock._onInk
                  : (enabled ? PrototypeDock._ink : PrototypeDock._dim),
              fontSize: 12,
              fontWeight: FontWeight.w500,
            ),
          ),
        ),
      ),
    );
  }
}

class _Separator extends StatelessWidget {
  const _Separator();

  @override
  Widget build(BuildContext context) => Container(
        width: 1,
        height: 20,
        margin: const EdgeInsets.symmetric(horizontal: 3),
        color: const Color(0xFF35322D),
      );
}
