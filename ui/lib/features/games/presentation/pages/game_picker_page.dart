import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/games/presentation/widgets/game_views.dart';
import 'package:kanpachi_ui/features/seed/presentation/ask_to_host.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// El selector de juego: lo instalado en esta PC, y la puerta a la biblioteca.
///
/// Muestra primero lo instalado porque es lo que casi siempre se busca, pero
/// la biblioteca entera está a un clic y bien visible: la detección ordena y
/// sugiere, jamás filtra. Un juego que Kanpachi no supo ver tiene que poder
/// elegirse igual.
class GamePickerScreen extends StatefulWidget {
  const GamePickerScreen({super.key});

  @override
  State<GamePickerScreen> createState() => _GamePickerScreenState();
}

class _GamePickerScreenState extends State<GamePickerScreen> {
  final TextEditingController _query = TextEditingController();

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final ShellState shellState = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;
    final bool fromRoom = shellState.pickerCameFromRoom;

    // El campo dice "buscar en el catálogo", así que busca en el catálogo
    // entero y no sólo en lo instalado: la detección ordena y sugiere, jamás
    // filtra. Sin texto se ve lo instalado, que es lo que casi siempre se
    // quiere y ahorra escribir.
    final String q = _query.text.trim().toLowerCase();
    final bool buscando = q.isNotEmpty;
    final List<Game> mostrados = buscando
        ? session.catalog
              .where((Game g) => g.name.toLowerCase().contains(q))
              .toList(growable: false)
        : session.installed;

    void pick(Game game) {
      context.read<SessionCubit>().proposeGame(game);
      shell.showDialog(AppDialog.confirmGame);
    }

    return ScreenBody(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ScreenHeader(
            title: fromRoom ? 'Elegir juego de la sala' : 'Crear sala',
            note: fromRoom
                ? 'La sala sigue igual: cambiar de juego solo cambia qué '
                      'puertos se abren.'
                : 'Puedes crear la sala sin juego y elegirlo adentro, o abrir '
                      'uno de una vez.',
            leading: AppBackButton(onPressed: shell.back),
          ),
          const SizedBox(height: AppSpacing.x4l),
          // El buscador y el conmutador comparten fila, como en el diseño. El
          // conmutador solo, alineado a la derecha sobre una fila vacía, dejaba
          // un hueco que no significaba nada.
          Row(
            children: <Widget>[
              Expanded(
                child: AppField(
                  controller: _query,
                  shape: AppFieldShape.pill,
                  height: 42,
                  hint: 'Buscar en el catálogo…',
                  onChanged: (_) => setState(() {}),
                ),
              ),
              const SizedBox(width: AppSpacing.lg),
              GameArtToggle(
                value: shellState.artMode,
                onChanged: shell.setArtMode,
                stretched: true,
              ),
            ],
          ),
          const SizedBox(height: AppSpacing.x4l),

          // Sólo al crear. Dentro de una sala ya existente no tiene sentido:
          // la sala ya está creada, y "sin juego" se consigue con la cruz de
          // la tarjeta del juego.
          if (!fromRoom) ...<Widget>[
            AppTappableCard(
              style: AppTappableCardStyle.dashed,
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpacing.x4l,
                vertical: AppSpacing.xxl,
              ),
              // Waits before navigating. See `SessionCubit.createRoom`: going
              // to the room screen with no room leaves a window with no way
              // out of it.
              // **Por `askToHost` y no por `createRoom` a pelo.** Este camino
              // abría una sala sin pasar por el diálogo de confianza, que es la
              // única puerta por la que se abre una, y sin preguntar por lo que
              // desplazaba: con una vuelta armada moría en «ya estás en una
              // sala» sin nada que pulsar. Quien abre de verdad es el diálogo,
              // así que acá ya no se navega: la pantalla de sala llega cuando
              // el daemon dice que hay sala.
              onTap: () => unawaited(
                askToHost(context, suggestedName: 'Sala de Kanpachi'),
              ),
              child: Row(
                children: <Widget>[
                  Expanded(
                    child: Text(
                      'Crear la sala sin juego y elegirlo adentro',
                      style: context.type.label.copyWith(color: colors.text),
                    ),
                  ),
                  Icon(Icons.arrow_forward, size: 17, color: colors.accent),
                ],
              ),
            ),
            const SizedBox(height: AppSpacing.x4l),
          ],

          AppKicker(
            buscando
                ? 'Resultados · ${mostrados.length}'
                : 'Instalados en esta PC · ${mostrados.length}',
          ),
          const SizedBox(height: AppSpacing.lg),
          GameCollection(
            games: mostrados,
            mode: shellState.artMode,
            selected: session.room?.game,
            onPick: pick,
          ),
          const SizedBox(height: AppSpacing.x6l),
          AppTappableCard(
            style: AppTappableCardStyle.sunken,
            padding: const EdgeInsets.symmetric(
              horizontal: AppSpacing.x4l,
              vertical: AppSpacing.x3l,
            ),
            onTap: () => shell.go(AppScreen.catalog),
            child: Row(
              children: <Widget>[
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(
                        'Ver toda la biblioteca · ${session.catalog.length} juegos',
                        style: context.type.strong.copyWith(color: colors.text),
                      ),
                      const SizedBox(height: 3),
                      Text(
                        'Con buscador y portadas. Si tu juego no aparece '
                        'arriba, está aquí igual.',
                        style: context.type.bodySm.copyWith(
                          color: colors.textMuted,
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: AppSpacing.x3l),
                Icon(Icons.arrow_forward, size: 17, color: colors.accent),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// La biblioteca completa, con buscador y alta manual.
class CatalogScreen extends StatefulWidget {
  const CatalogScreen({super.key});

  @override
  State<CatalogScreen> createState() => _CatalogScreenState();
}

class _CatalogScreenState extends State<CatalogScreen> {
  final TextEditingController _query = TextEditingController();

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final ShellCubit shell = context.read<ShellCubit>();
    final ShellState shellState = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;

    final String q = _query.text.trim().toLowerCase();
    final List<Game> results = q.isEmpty
        ? session.catalog
        : session.catalog
              .where((Game g) => g.name.toLowerCase().contains(q))
              .toList(growable: false);

    // La única pantalla que NO usa `ScreenBody`: su cabecera se queda fija y
    // sólo se recorre la rejilla. Con 18 juegos ya hay que bajar, y perder de
    // vista el buscador justo cuando se está buscando es lo que obliga a subir
    // otra vez para escribir una letra más. El filete inferior llega de borde
    // a borde de la ventana; el contenido, en cambio, va centrado y con el
    // mismo tope que el resto de la app.
    return ScreenEnter(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          DecoratedBox(
            decoration: BoxDecoration(
              color: colors.surface,
              border: Border(
                bottom: BorderSide(
                  color: colors.border,
                  width: AppStroke.hairline,
                ),
              ),
            ),
            child: Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(
                  maxWidth: AppSpacing.contentMax,
                ),
                child: Padding(
                  padding: const EdgeInsets.fromLTRB(
                    AppSpacing.pageInline,
                    AppSpacing.x3l,
                    AppSpacing.pageInline,
                    AppSpacing.xl,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      ScreenHeader(
                        title: 'Biblioteca de juegos',
                        leading: AppBackButton(onPressed: shell.back),
                        trailing: Text(
                          q.isEmpty
                              ? '${session.catalog.length} juegos'
                              : '${results.length} de '
                                    '${session.catalog.length}',
                          style: context.type.monoSm.copyWith(
                            color: colors.textMuted,
                          ),
                        ),
                      ),
                      const SizedBox(height: AppSpacing.xl),
                      Row(
                        children: <Widget>[
                          Expanded(
                            child: AppField(
                              controller: _query,
                              shape: AppFieldShape.pill,
                              height: 42,
                              hint: 'Buscar por nombre…',
                              onChanged: (_) => setState(() {}),
                            ),
                          ),
                          const SizedBox(width: AppSpacing.lg),
                          // Que el alta manual viva junto al buscador no es casual: es el
                          // sitio donde alguien acaba de comprobar que su juego no está.
                          AppButton(
                            label: 'Agregar juego',
                            // Hundido y no chip: va a ras del buscador, que también lo
                            // es, y con el chip parecía flotar sobre él.
                            variant: AppButtonVariant.quietSunken,
                            height: 42,
                            textStyle: context.type.strongSm.copyWith(
                              fontSize: 13,
                              height: 1,
                            ),
                            horizontalPadding: AppSpacing.x4l,
                            icon: const Icon(Icons.add),
                            onPressed: () => shell.go(AppScreen.manualGame),
                          ),
                          const SizedBox(width: AppSpacing.lg),
                          GameArtToggle(
                            value: shellState.artMode,
                            onChanged: shell.setArtMode,
                            stretched: true,
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
          Expanded(
            child: SingleChildScrollView(
              padding: const EdgeInsets.fromLTRB(
                AppSpacing.pageInline,
                AppSpacing.x4l,
                AppSpacing.pageInline,
                AppSpacing.x7l,
              ),
              child: Center(
                child: ConstrainedBox(
                  constraints: const BoxConstraints(
                    maxWidth: AppSpacing.contentMax,
                  ),
                  child: results.isEmpty
                      ? const _NoResults()
                      : GameCollection(
                          games: results,
                          mode: shellState.artMode,
                          selected: session.room?.game,
                          showInstalledBadge: true,
                          minTileWidth: 140,
                          onPick: (Game game) {
                            context.read<SessionCubit>().proposeGame(game);
                            shell.showDialog(AppDialog.confirmGame);
                          },
                        ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Cuando la búsqueda no encuentra nada.
///
/// No dice sólo "no hay": dice qué hacer. Que un juego falte del catálogo es
/// esperable — son cinco los que vienen probados — y el creador de perfiles
/// existe justo para eso.
class _NoResults extends StatelessWidget {
  const _NoResults();

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Padding(
      padding: const EdgeInsets.symmetric(
        vertical: 50,
        horizontal: AppSpacing.x5l,
      ),
      child: Column(
        children: <Widget>[
          Text(
            'Ningún juego coincide',
            style: context.type.strong.copyWith(
              color: colors.text,
              fontSize: 16,
            ),
          ),
          const SizedBox(height: AppSpacing.md),
          ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 380),
            child: Text(
              'Si el juego no está en el catálogo, se puede crear su perfil '
              'con el creador de perfiles y compartirlo como un .json.',
              textAlign: TextAlign.center,
              style: context.type.body.copyWith(color: colors.textMuted),
            ),
          ),
        ],
      ),
    );
  }
}
