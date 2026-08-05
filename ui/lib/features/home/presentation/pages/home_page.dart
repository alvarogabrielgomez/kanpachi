import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_divider.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/features/games/presentation/widgets/game_views.dart';
import 'package:kanpachi_ui/core/design_system/molecules/app_list.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/messages/app_message_notice.dart';
import 'package:kanpachi_ui/core/messages/message_catalog.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/domain/entities/health.dart';
import 'package:kanpachi_ui/features/session/domain/invite_code.dart';
import 'package:kanpachi_ui/features/session/domain/room_names.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// La portada: sin sala.
///
/// Dos caminos y nada más — entrar con un código que te pasaron, o crear una
/// sala. Lo demás de la pantalla es tu biblioteca y los avisos de salud, que
/// no son acciones sino contexto.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  final TextEditingController _code = TextEditingController();
  final TextEditingController _roomName = TextEditingController();

  /// Se sortea una vez y se queda. Volver a sortearlo en cada rebuild haría
  /// que el nombre sugerido bailara mientras se escribe el código de al lado.
  late final String _nameHint = RoomNames.suggest();

  @override
  void dispose() {
    _code.dispose();
    _roomName.dispose();
    super.dispose();
  }

  void _onCodeChanged(String raw) {
    final String masked = InviteCode.mask(raw);
    if (masked != _code.text) {
      _code.value = TextEditingValue(
        text: masked,
        selection: TextSelection.collapsed(offset: masked.length),
      );
    }
    setState(() {});
  }

  void _join() {
    if (!InviteCode.isComplete(_code.text)) return;
    context.read<SessionCubit>().joinRoom(_code.text);
    context.read<ShellCubit>().go(AppScreen.room);
  }

  void _createEmpty() {
    final String name = _roomName.text.trim().isEmpty
        ? _nameHint
        : _roomName.text.trim();
    context.read<SessionCubit>().createRoom(name: name);
    context.read<ShellCubit>().go(AppScreen.room);
  }

  @override
  Widget build(BuildContext context) {
    final ShellState shell = context.watch<ShellCubit>().state;
    final SessionState session = context.watch<SessionCubit>().state;
    final bool canJoin = InviteCode.isComplete(_code.text);

    return ScreenBody(
      child: LayoutBuilder(
        builder: (BuildContext context, BoxConstraints constraints) {
          final bool wide = constraints.maxWidth >= 640;
          final Widget left = _JoinAndCreate(
            code: _code,
            roomName: _roomName,
            nameHint: _nameHint,
            canJoin: canJoin,
            showAlerts: shell.showHealthAlerts,
            alerts: session.health.alerts,
            onCodeChanged: _onCodeChanged,
            onJoin: _join,
            onCreate: _createEmpty,
          );
          final Widget right = _MyGames(
            games: session.installed,
            artMode: shell.artMode,
            total: session.catalog.length,
          );

          if (!wide) {
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                left,
                const SizedBox(height: AppSpacing.x9l),
                right,
              ],
            );
          }
          return Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Expanded(child: left),
              const SizedBox(width: AppSpacing.x9l),
              Expanded(child: right),
            ],
          );
        },
      ),
    );
  }
}

class _JoinAndCreate extends StatelessWidget {
  const _JoinAndCreate({
    required this.code,
    required this.roomName,
    required this.nameHint,
    required this.canJoin,
    required this.showAlerts,
    required this.alerts,
    required this.onCodeChanged,
    required this.onJoin,
    required this.onCreate,
  });

  final TextEditingController code;
  final TextEditingController roomName;
  final String nameHint;
  final bool canJoin;

  /// El interruptor de la barra de prototipo. Decide si la sección se enseña,
  /// jamás qué dice.
  final bool showAlerts;

  /// Los avisos que mandó el daemon. Si no mandó ninguno no se pinta nada, que
  /// es el caso normal en una máquina sana.
  final List<HealthAlert> alerts;
  final ValueChanged<String> onCodeChanged;
  final VoidCallback onJoin;
  final VoidCallback onCreate;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        const AppKicker('Código de sala'),
        const SizedBox(height: 9),
        AppField(
          controller: code,
          shape: AppFieldShape.pill,
          mono: true,
          hint: 'A7K2-M9QX',
          maxLength: InviteCode.length + 1,
          onChanged: onCodeChanged,
          onSubmitted: (_) => onJoin(),
          inputFormatters: <TextInputFormatter>[
            FilteringTextInputFormatter.allow(RegExp('[a-zA-Z0-9-]')),
          ],
          trailing: AppButton(
            label: 'Unirse',
            height: 42,
            variant: AppButtonVariant.primaryFlat,
            // Apagado hasta que el código esté completo. Dejarlo encendido
            // llevaría a una espera que ya se sabe que va a fallar.
            onPressed: canJoin ? onJoin : null,
          ),
        ),
        const SizedBox(height: AppSpacing.x3l),
        const AppLabeledDivider('o'),
        const SizedBox(height: AppSpacing.x3l),
        const AppKicker('Nueva sala'),
        const SizedBox(height: 9),
        AppField(
          controller: roomName,
          shape: AppFieldShape.pill,
          hint: nameHint,
          maxLength: 24,
          onSubmitted: (_) => onCreate(),
          trailing: AppButton(
            label: 'Crear sala',
            height: 42,
            variant: AppButtonVariant.primaryFlat,
            onPressed: onCreate,
          ),
        ),
        const SizedBox(height: AppSpacing.md),
        Text(
          'La sala se crea vacía: el juego se elige adentro y se puede cambiar.',
          textAlign: TextAlign.center,
          style: context.type.bodySm.copyWith(color: colors.textMuted),
        ),
        if (showAlerts && alerts.isNotEmpty) ...<Widget>[
          const SizedBox(height: AppSpacing.x5l),
          _HealthAlerts(alerts: alerts),
        ],
      ],
    );
  }
}

/// Los avisos de salud de la máquina.
///
/// Kanpachi los mira porque su promesa depende de ellos: se apoya en el
/// Firewall de Windows para que nadie de la sala alcance el resto de tu PC, y
/// no puede cumplirla si está apagado. Y avisa de un puerto abierto en el
/// router aunque no sea cosa suya, porque es justo lo que Kanpachi existe para
/// no tener que hacer.
///
/// **El texto no está acá y la lista tampoco.** El texto lo trae `AppMessages`,
/// que es el único sitio del programa donde se escribe copy de aviso, y la lista
/// la manda el daemon, que es el único que puede medir lo que hay. Esta clase
/// solo las apila.
///
/// El orden es el del daemon, que las produce por gravedad. Reordenarlas acá
/// sería que la pantalla opine sobre algo ya decidido donde se puede medir.
///
/// Se pintan **por la cadena del cable** y no por la clase, y esa elección es la
/// que impide perder un aviso: una clave que esta versión de la UI no conozca
/// sale igual con el mensaje de reserva y con el detalle del daemon pegado. Se
/// pierde el copy bueno, no se pierde el aviso.
class _HealthAlerts extends StatelessWidget {
  const _HealthAlerts({required this.alerts});

  final List<HealthAlert> alerts;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: <Widget>[
        for (int i = 0; i < alerts.length; i++) ...<Widget>[
          if (i > 0) const SizedBox(height: AppSpacing.lg),
          AppMessageNotice(
            message: AppMessages.alertFromWire(
              alerts[i].wire,
              detail: alerts[i].detail,
            ),
            titleStyle: context.type.strongSm,
          ),
        ],
      ],
    );
  }
}

/// Los juegos detectados en esta PC.
class _MyGames extends StatelessWidget {
  const _MyGames({
    required this.games,
    required this.artMode,
    required this.total,
  });

  final List<Game> games;
  final GameArtMode artMode;
  final int total;

  @override
  Widget build(BuildContext context) {
    final ShellCubit shell = context.read<ShellCubit>();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            const AppKicker('Tus juegos'),
            const Spacer(),
            // El mismo widget que usan el selector y la biblioteca. Estaba
            // duplicado en línea, así que arreglar los iconos en un sitio
            // dejaba los otros dos atrás.
            GameArtToggle(value: artMode, onChanged: shell.setArtMode),
          ],
        ),
        const SizedBox(height: 9),
        AppRowList(
          footer: AppRow(
            background: context.colors.surfaceSunken,
            onTap: () => shell.openGamePicker(fromRoom: false),
            child: Text(
              'Ver toda la biblioteca ($total)',
              style: context.type.label.copyWith(
                fontSize: 13,
                color: context.colors.accent,
              ),
            ),
          ),
          children: <Widget>[
            for (final Game game in games)
              AppRow(
                onTap: () => shell.openGamePicker(fromRoom: false),
                child: _GameRow(
                  game: game,
                  showCover: artMode == GameArtMode.cover,
                ),
              ),
          ],
        ),
      ],
    );
  }
}

class _GameRow extends StatelessWidget {
  const _GameRow({required this.game, required this.showCover});

  final Game game;
  final bool showCover;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Row(
      children: <Widget>[
        if (showCover) ...<Widget>[
          const AppCover.thumb(),
          const SizedBox(width: AppSpacing.xl),
        ],
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                game.name,
                // 14 y no 13,5: la lista de la portada va un punto por encima
                // de las del selector y el catálogo, que sí son 13,5. Local a
                // esta fila, que es privada de la pantalla.
                style: context.type.label.copyWith(
                  fontSize: 14,
                  color: colors.text,
                ),
              ),
              Text(
                game.portsLabel,
                style: context.type.monoXs.copyWith(color: colors.textMuted),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
