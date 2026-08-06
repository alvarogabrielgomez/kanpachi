import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/tokens/density_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/room.dart';

/// Las pantallas de la app.
///
/// La navegación es un enum y no un router de rutas con URL a propósito: esto
/// es una ventana de escritorio sin barra de direcciones, sin historial que el
/// usuario pueda manipular y sin enlaces profundos más allá del `kanpachi://`
/// que entra por el sistema. Un `go_router` acá añadiría un modelo mental
/// entero para resolver un problema que no tenemos.
enum AppScreen {
  welcome,
  nickname,
  home,
  gamePicker,
  catalog,
  manualGame,
  room,

  /// Lo que esta PC tiene abierto, MEDIDO. Se llega desde la sala, y no está
  /// dentro de ella porque no es información de la sala: es de la máquina, y
  /// sigue siendo cierta sin sala abierta.
  exposure,
  invite,

  /// Los dos ajustes que hay. Se llega por el engranaje del menú de cuenta, y
  /// desde ningún otro sitio: nada de acá hace falta para jugar.
  settings,
}

/// Los diálogos, que se dibujan por encima de la pantalla actual.
enum AppDialog { none, confirmGame, confirmKick, confirmRenew }

/// Cómo se listan los juegos.
enum GameArtMode { cover, list }

@immutable
class ShellState {
  const ShellState({
    this.screen = AppScreen.welcome,
    this.dialog = AppDialog.none,
    this.themeMode = ThemeMode.system,
    this.density = AppDensity.balanced,
    this.artMode = GameArtMode.cover,
    this.ambient = true,
    this.accountMenuOpen = false,
    this.pickerCameFromRoom = false,
    this.kickTarget,
  });

  final AppScreen screen;
  final AppDialog dialog;
  final ThemeMode themeMode;
  final AppDensity density;
  final GameArtMode artMode;

  /// El fondo en movimiento. Se puede apagar: corre durante horas mientras
  /// alguien juega, y a quien le molesta el movimiento le molesta de verdad.
  final bool ambient;

  final bool accountMenuOpen;

  /// De dónde se abrió el selector de juego. Cambia el título, el texto y a
  /// dónde vuelve la flecha: no es lo mismo elegir juego PARA crear una sala
  /// que cambiar el de una sala que ya existe y tiene gente dentro.
  final bool pickerCameFromRoom;

  final Member? kickTarget;

  ShellState copyWith({
    AppScreen? screen,
    AppDialog? dialog,
    ThemeMode? themeMode,
    AppDensity? density,
    GameArtMode? artMode,
    bool? ambient,
    bool? accountMenuOpen,
    bool? pickerCameFromRoom,
    Member? kickTarget,
    bool clearKickTarget = false,
  }) => ShellState(
    screen: screen ?? this.screen,
    dialog: dialog ?? this.dialog,
    themeMode: themeMode ?? this.themeMode,
    density: density ?? this.density,
    artMode: artMode ?? this.artMode,
    ambient: ambient ?? this.ambient,
    accountMenuOpen: accountMenuOpen ?? this.accountMenuOpen,
    pickerCameFromRoom: pickerCameFromRoom ?? this.pickerCameFromRoom,
    kickTarget: clearKickTarget ? null : (kickTarget ?? this.kickTarget),
  );
}

/// Navegación y preferencias de presentación.
///
/// Está separado de `SessionCubit` porque son dos cosas con dueños distintos:
/// la sesión la manda el daemon y sobrevive a que se cierre la ventana; esto
/// es de la ventana y sólo de la ventana.
class ShellCubit extends Cubit<ShellState> {
  ShellCubit({AppScreen initial = AppScreen.welcome})
    : super(ShellState(screen: initial));

  void go(AppScreen screen) => emit(
    state.copyWith(
      screen: screen,
      dialog: AppDialog.none,
      accountMenuOpen: false,
    ),
  );

  /// Abre el selector recordando de dónde vino.
  void openGamePicker({required bool fromRoom}) => emit(
    state.copyWith(
      screen: AppScreen.gamePicker,
      dialog: AppDialog.none,
      pickerCameFromRoom: fromRoom,
      accountMenuOpen: false,
    ),
  );

  void showDialog(AppDialog dialog) => emit(state.copyWith(dialog: dialog));

  void closeDialog() =>
      emit(state.copyWith(dialog: AppDialog.none, clearKickTarget: true));

  void askKick(Member member) =>
      emit(state.copyWith(dialog: AppDialog.confirmKick, kickTarget: member));

  void toggleAccountMenu() =>
      emit(state.copyWith(accountMenuOpen: !state.accountMenuOpen));

  void closeAccountMenu() => emit(state.copyWith(accountMenuOpen: false));

  void setArtMode(GameArtMode mode) => emit(state.copyWith(artMode: mode));

  void setDensity(AppDensity density) => emit(state.copyWith(density: density));

  void setAmbient({required bool enabled}) =>
      emit(state.copyWith(ambient: enabled));

  void cycleTheme() => emit(
    state.copyWith(
      themeMode: switch (state.themeMode) {
        ThemeMode.system => ThemeMode.light,
        ThemeMode.light => ThemeMode.dark,
        ThemeMode.dark => ThemeMode.system,
      },
    ),
  );

  void cycleDensity() => emit(
    state.copyWith(
      density: switch (state.density) {
        AppDensity.airy => AppDensity.balanced,
        AppDensity.balanced => AppDensity.dense,
        AppDensity.dense => AppDensity.airy,
      },
    ),
  );
}
