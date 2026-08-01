import 'package:flutter/material.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_segmented.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// El conmutador portadas/lista. Está acá y no repetido en cada pantalla
/// porque aparece en tres y tiene que verse y comportarse igual en las tres.
class GameArtToggle extends StatelessWidget {
  const GameArtToggle({
    required this.value,
    required this.onChanged,
    this.stretched = false,
    super.key,
  });

  final GameArtMode value;
  final ValueChanged<GameArtMode> onChanged;

  /// Al lado de un buscador, el control se estira a lo alto de la fila.
  final bool stretched;

  @override
  Widget build(BuildContext context) {
    return AppSegmented<GameArtMode>(
      value: value,
      onChanged: onChanged,
      itemSize: stretched ? 44 : 26,
      itemWidth: stretched ? 42 : null,
      segments: const <AppSegment<GameArtMode>>[
        AppSegment<GameArtMode>(
          value: GameArtMode.cover,
          icon: Icons.grid_view,
          tooltip: 'Portadas',
        ),
        AppSegment<GameArtMode>(
          value: GameArtMode.list,
          icon: Icons.notes,
          tooltip: 'Lista',
        ),
      ],
    );
  }
}

/// Los juegos, en la forma que se haya elegido.
///
/// Un solo widget para las dos formas y para las tres pantallas: son la misma
/// lista mirada distinto, y tenerlas duplicadas garantizaría que una se quede
/// atrás cuando cambie el contenido de una tarjeta.
class GameCollection extends StatelessWidget {
  const GameCollection({
    required this.games,
    required this.mode,
    required this.onPick,
    this.selected,
    this.showInstalledBadge = false,
    this.minTileWidth = 150,
    super.key,
  });

  final List<Game> games;
  final GameArtMode mode;
  final ValueChanged<Game> onPick;

  /// El juego que la sala ya tiene puesto, marcado con el acento.
  final Game? selected;

  /// En la biblioteca sí, en el selector no: allí todos están instalados y la
  /// etiqueta sería ruido en todas las tarjetas.
  final bool showInstalledBadge;

  final double minTileWidth;

  @override
  Widget build(BuildContext context) {
    if (mode == GameArtMode.list) {
      return AppRowList(
        children: <Widget>[
          for (final Game game in games)
            AppRow(
              onTap: () => onPick(game),
              child: _GameLine(
                game: game,
                showInstalledBadge: showInstalledBadge,
              ),
            ),
        ],
      );
    }

    // Wrap y no GridView. Un grid obliga a declarar el alto de la celda por
    // adelantado (`mainAxisExtent`), y ese número es una suma a mano de la
    // portada más dos renglones de texto: cambia con el tema, con la fuente y
    // con la escala de texto del sistema, y cuando se queda un píxel corto la
    // ficha se raya de amarillo y negro. El Wrap deja que la ficha mida lo que
    // mide. Aquí no se pierde nada por no ser perezoso: esto vive dentro de un
    // SingleChildScrollView, así que el grid tampoco lo era.
    return LayoutBuilder(
      builder: (BuildContext context, BoxConstraints constraints) {
        const double gap = AppSpacing.xl;
        final int columns =
            ((constraints.maxWidth + gap) / (minTileWidth + gap))
                .floor()
                .clamp(1, 8);
        final double tile =
            (constraints.maxWidth - gap * (columns - 1)) / columns;
        return Wrap(
          spacing: gap,
          runSpacing: gap,
          children: <Widget>[
            for (final Game game in games)
              SizedBox(
                width: tile,
                child: _GameTile(
                  game: game,
                  selected: game == selected,
                  showInstalledBadge: showInstalledBadge,
                  onTap: () => onPick(game),
                ),
              ),
          ],
        );
      },
    );
  }
}

class _GameTile extends StatelessWidget {
  const _GameTile({
    required this.game,
    required this.selected,
    required this.showInstalledBadge,
    required this.onTap,
  });

  final Game game;
  final bool selected;
  final bool showInstalledBadge;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return AppTappableCard(
      onTap: onTap,
      selected: selected,
      padding: const EdgeInsets.all(AppSpacing.xl),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          AppCover.grid(
            badge: showInstalledBadge && game.installed
                ? const AppInstalledBadge()
                : null,
          ),
          const SizedBox(height: AppSpacing.lg),
          Text(
            game.name,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: context.type.strongSm.copyWith(color: colors.text),
          ),
          const SizedBox(height: 2),
          Text(
            game.portsLabel,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: context.type.monoXxs.copyWith(color: colors.textMuted),
          ),
        ],
      ),
    );
  }
}

class _GameLine extends StatelessWidget {
  const _GameLine({required this.game, required this.showInstalledBadge});

  final Game game;
  final bool showInstalledBadge;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      children: <Widget>[
        Expanded(
          child: Text(
            game.name,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: context.type.label.copyWith(color: colors.text),
          ),
        ),
        if (showInstalledBadge && game.installed) ...<Widget>[
          Text(
            'INSTALADO',
            style: context.type.monoXxs.copyWith(
              color: colors.ok,
              fontSize: 9.5,
              fontWeight: FontWeight.w600,
              letterSpacing: 0.76,
            ),
          ),
          const SizedBox(width: AppSpacing.xl),
        ],
        Text(
          game.portsLabel,
          style: context.type.monoXs.copyWith(color: colors.textMuted),
        ),
      ],
    );
  }
}
