import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_cover.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_segmented.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/game.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// Dar de alta un juego que no está en el catálogo.
///
/// Kanpachi sólo necesita los puertos que el juego abre en tu PC. Nada más:
/// ni la ruta del ejecutable, ni el ID de Steam, ni permiso para mirarlo
/// correr. Que el formulario pida tan poco es la prueba de que la app no
/// necesita saber más.
class ManualGameScreen extends StatefulWidget {
  const ManualGameScreen({super.key});

  @override
  State<ManualGameScreen> createState() => _ManualGameScreenState();
}

class _ManualGameScreenState extends State<ManualGameScreen> {
  final TextEditingController _name = TextEditingController();
  final TextEditingController _cover = TextEditingController();

  /// Empieza con una fila: un formulario que arranca vacío obliga a descubrir
  /// que hay que pulsar "añadir" antes de poder escribir nada.
  final List<_RuleDraft> _rules = <_RuleDraft>[_RuleDraft()];

  @override
  void dispose() {
    _name.dispose();
    _cover.dispose();
    for (final _RuleDraft rule in _rules) {
      rule.dispose();
    }
    super.dispose();
  }

  List<PortRule> get _portRules => _rules
      .where((_RuleDraft r) => r.controller.text.trim().isNotEmpty)
      .map(
        (_RuleDraft r) =>
            PortRule(range: r.controller.text.trim(), protocol: r.protocol),
      )
      .toList(growable: false);

  void _save() {
    final SessionCubit session = context.read<SessionCubit>();
    session.saveManualGame(
      Game(
        name: _name.text.trim().isEmpty
            ? 'Juego sin nombre'
            : _name.text.trim(),
        rules: _portRules,
        manual: true,
        coverUrl: _cover.text.trim().isEmpty ? null : _cover.text.trim(),
      ),
    );
    context.read<ShellCubit>().go(AppScreen.catalog);
  }

  @override
  Widget build(BuildContext context) {
    final ShellCubit shell = context.read<ShellCubit>();

    return ScreenBody(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ScreenHeader(
            title: 'Agregar un juego',
            note:
                'Para lo que no está en el catálogo. Kanpachi solo necesita '
                'los puertos que el juego abre en tu PC.',
            noteMaxWidth: 520,
            leading: AppBackButton(
              onPressed: () => shell.go(AppScreen.catalog),
            ),
          ),
          const SizedBox(height: AppSpacing.x7l),
          // El `Align` no es decorativo: la Column padre estira a lo ancho con
          // constraints TIRANTES, así que un ConstrainedBox pelado se iría a
          // los 940 igual y el tope no haría nada.
          Align(
            alignment: Alignment.centerLeft,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 860),
              child: LayoutBuilder(
                builder: (BuildContext context, BoxConstraints constraints) {
                  final Widget form = _Form(
                    name: _name,
                    cover: _cover,
                    rules: _rules,
                    onChanged: () => setState(() {}),
                    onAddRule: () => setState(() => _rules.add(_RuleDraft())),
                    onRemoveRule: (int i) => setState(() {
                      _rules.removeAt(i).dispose();
                      if (_rules.isEmpty) _rules.add(_RuleDraft());
                    }),
                    onCancel: () => shell.go(AppScreen.catalog),
                    onSave: _save,
                  );
                  final Widget preview = _Preview(
                    name: _name.text.trim(),
                    rules: _portRules,
                  );
                  if (constraints.maxWidth < 620) {
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: <Widget>[
                        form,
                        const SizedBox(height: AppSpacing.x8l),
                        preview,
                      ],
                    );
                  }
                  return Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Expanded(child: form),
                      const SizedBox(width: AppSpacing.x8l),
                      Expanded(child: preview),
                    ],
                  );
                },
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Una fila de puertos mientras se escribe.
class _RuleDraft {
  final TextEditingController controller = TextEditingController();
  PortProtocol protocol = PortProtocol.udp;

  void dispose() => controller.dispose();
}

class _Form extends StatelessWidget {
  const _Form({
    required this.name,
    required this.cover,
    required this.rules,
    required this.onChanged,
    required this.onAddRule,
    required this.onRemoveRule,
    required this.onCancel,
    required this.onSave,
  });

  final TextEditingController name;
  final TextEditingController cover;
  final List<_RuleDraft> rules;
  final VoidCallback onChanged;
  final VoidCallback onAddRule;
  final ValueChanged<int> onRemoveRule;
  final VoidCallback onCancel;
  final VoidCallback onSave;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        const AppKicker('Nombre', small: true),
        const SizedBox(height: AppSpacing.md),
        AppField(
          controller: name,
          hint: 'Valheim',
          onChanged: (_) => onChanged(),
        ),
        const SizedBox(height: 7),
        Text(
          'Como lo verán tus panas en la sala.',
          style: context.type.bodySm.copyWith(color: colors.textMuted),
        ),
        const SizedBox(height: AppSpacing.x5l),
        const AppKicker('Puertos', small: true),
        const SizedBox(height: AppSpacing.md),
        for (int i = 0; i < rules.length; i++) ...<Widget>[
          _RuleRow(
            draft: rules[i],
            onChanged: onChanged,
            onRemove: () => onRemoveRule(i),
          ),
          const SizedBox(height: AppSpacing.md),
        ],
        Align(
          alignment: Alignment.centerLeft,
          // Trazo discontinuo, como en el diseño: dice "acá puede haber otra
          // fila y todavía no la hay", que es lo mismo que dice el hueco de
          // portada. Un borde continuo lo haría parecer una acción más del
          // formulario.
          child: CustomPaint(
            foregroundPainter: DashedBorderPainter(
              color: colors.border,
              radius: AppRadius.pill,
            ),
            child: AppButton(
              label: 'Añadir otro rango',
              variant: AppButtonVariant.ghostDashed,
              height: 36,
              horizontalPadding: AppSpacing.x3l,
              textStyle: context.type.label.copyWith(fontSize: 13, height: 1),
              icon: const Icon(Icons.add),
              onPressed: onAddRule,
            ),
          ),
        ),
        const SizedBox(height: 7),
        Text(
          'Un puerto (27015) o un rango (2456-2458). Añade una fila por cada '
          'protocolo distinto.',
          style: context.type.bodySm.copyWith(color: colors.textMuted),
        ),
        const SizedBox(height: AppSpacing.x5l),
        const AppKicker('Portada (SteamDB)', small: true),
        const SizedBox(height: AppSpacing.md),
        AppField(
          controller: cover,
          hint: 'https://steamdb.info/app/892970/',
          onChanged: (_) => onChanged(),
        ),
        const SizedBox(height: 7),
        Text(
          'Opcional. Pega el enlace de SteamDB y se toma la portada.',
          style: context.type.bodySm.copyWith(color: colors.textMuted),
        ),
        const SizedBox(height: AppSpacing.x7l),
        Row(
          children: <Widget>[
            AppButton(
              label: 'Cancelar',
              variant: AppButtonVariant.ghost,
              height: 46,
              // El único ghost que el diseño pinta en color de texto pleno:
              // acá compite con «Guardar juego» al lado y tiene que pesar lo
              // mismo. En los otros siete sitios va apagado.
              emphasis: true,
              textStyle: context.type.label.copyWith(fontSize: 14),
              onPressed: onCancel,
            ),
            const SizedBox(width: AppSpacing.lg),
            Expanded(
              child: AppButton(
                label: 'Guardar juego',
                variant: AppButtonVariant.primaryFlat,
                height: 46,
                onPressed: onSave,
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _RuleRow extends StatelessWidget {
  const _RuleRow({
    required this.draft,
    required this.onChanged,
    required this.onRemove,
  });

  final _RuleDraft draft;
  final VoidCallback onChanged;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    return AppField(
      controller: draft.controller,
      shape: AppFieldShape.inline,
      mono: true,
      hint: '2456-2458',
      onChanged: (_) => onChanged(),
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          // Con palabras y no con iconos: el protocolo es el dato que decide
          // qué se abre, y no hay dibujo que diga "UDP". Con iconos había que
          // parar el ratón encima para saber qué estaba puesto.
          AppSegmented<PortProtocol>(
            value: draft.protocol,
            itemSize: 28,
            onChanged: (PortProtocol p) {
              draft.protocol = p;
              onChanged();
            },
            segments: const <AppSegment<PortProtocol>>[
              AppSegment<PortProtocol>(
                value: PortProtocol.tcp,
                label: 'TCP',
                tooltip: 'Sólo TCP',
              ),
              AppSegment<PortProtocol>(
                value: PortProtocol.udp,
                label: 'UDP',
                tooltip: 'Sólo UDP',
              ),
              AppSegment<PortProtocol>(
                value: PortProtocol.both,
                label: 'AMBOS',
                tooltip: 'TCP y UDP',
              ),
            ],
          ),
          AppIconButton(
            icon: Icons.close,
            tooltip: 'Quitar este rango',
            width: 30,
            height: 30,
            danger: true,
            onPressed: onRemove,
          ),
        ],
      ),
    );
  }
}

/// La vista previa: exactamente la misma tarjeta que se va a ver en la
/// biblioteca, para que no haya sorpresa entre lo que se escribe y lo que
/// queda guardado.
class _Preview extends StatelessWidget {
  const _Preview({required this.name, required this.rules});

  final String name;
  final List<PortRule> rules;

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        const AppKicker('Vista previa', small: true),
        const SizedBox(height: AppSpacing.md),
        ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 220),
          child: AppCard(
            radius: AppRadius.allXl,
            padding: const EdgeInsets.all(AppSpacing.xxl),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                const AppCover.preview(),
                const SizedBox(height: AppSpacing.xl),
                Text(
                  name.isEmpty ? 'Nombre del juego' : name,
                  style: context.type.strong.copyWith(color: colors.text),
                ),
                const SizedBox(height: 3),
                Text(
                  rules.isEmpty
                      ? '— puertos —'
                      : rules.map((PortRule r) => r.label).join(' · '),
                  style: context.type.monoXs.copyWith(color: colors.textMuted),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}
