import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_field.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_chip.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/domain/entities/own_seed.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// A qué registro le pide sala esta máquina.
///
/// # Por qué hay que preguntarlo, y por qué solo al que hospeda
///
/// Porque Kanpachi dejó de tener un registro de fábrica. El registro es el
/// punto de encuentro de una sala, lo levanta cualquiera, y viaja dentro de
/// cada código: al ENTRAR sale del código que te pegaron, y al ABRIR no hay
/// código todavía, porque el código es justo lo que el registro emite.
///
/// # Por qué entrar a la sala de alguien no lo configura solo
///
/// Se probó ese camino y es peor de lo que parece: la siguiente sala que abras
/// se hospedaría en el servidor de un desconocido sin que nadie lo decidiera, y
/// el diálogo de confianza te llegaría con ese nombre ya puesto. Lo que sí hace
/// es SUGERIRLO acá, marcado como lo que es, para no obligar a nadie a ir a
/// buscar el nombre a un chat.
class SeedScreen extends StatefulWidget {
  const SeedScreen({super.key});

  @override
  State<SeedScreen> createState() => _SeedScreenState();
}

class _SeedScreenState extends State<SeedScreen> {
  final TextEditingController _controller = TextEditingController();

  /// Lo que había guardado y lo que se puede sugerir. Nulo mientras se lee.
  OwnSeed? _actual;

  /// Lo que dijo el daemon al rechazar. Se enseña PEGADO al campo, y no como
  /// aviso de la portada: el error es de lo que esa persona acaba de escribir.
  String? _error;

  bool _guardando = false;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_alEscribir);
    unawaited(_leer());
  }

  @override
  void dispose() {
    _controller.removeListener(_alEscribir);
    _controller.dispose();
    super.dispose();
  }

  /// Redibuja el botón, y BORRA el error anterior.
  ///
  /// Dejarlo puesto mientras alguien corrige lo que escribió es acusar al texto
  /// nuevo de lo que hizo mal el viejo.
  void _alEscribir() => setState(() => _error = null);

  Future<void> _leer() async {
    try {
      final OwnSeed v = await context.read<SessionCubit>().ownSeed();
      if (!mounted) return;
      setState(() {
        _actual = v;
        // Lo configurado gana a la sugerencia: quien ya eligió uno viene a
        // cambiarlo, y encontrarse el de otro en el campo sería proponerle
        // justo lo que no pidió.
        _controller.text = v.configured.isNotEmpty ? v.configured : v.suggested;
      });
    } on Object catch (e) {
      if (!mounted) return;
      setState(() => _error = e.toString());
    }
  }

  bool get _valido => _controller.text.trim().isNotEmpty && !_guardando;

  /// Guarda y vuelve por donde se vino.
  ///
  /// El nombre lo valida el DAEMON, que es quien tiene la misma comprobación
  /// que aplica al registro de un código pegado. Acá solo se exige que haya
  /// algo escrito: repetir la regla de un nombre de dominio en Dart daría dos
  /// sitios donde arreglarla y uno de los dos se olvidaría.
  Future<void> _guardar() async {
    if (!_valido) return;
    setState(() => _guardando = true);
    try {
      await context.read<SessionCubit>().ownSeed(seed: _controller.text.trim());
      if (!mounted) return;
      context.read<ShellCubit>().back();
    } on Object catch (e) {
      if (!mounted) return;
      setState(() {
        _guardando = false;
        _error = e.toString();
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final OwnSeed? actual = _actual;
    final bool sugerido =
        actual != null &&
        actual.configured.isEmpty &&
        actual.suggested.isNotEmpty;

    return ScreenCentered(
      child: Column(
        children: <Widget>[
          Text(
            '¿En qué servidor abres tus salas?',
            textAlign: TextAlign.center,
            style: context.type.display.copyWith(color: colors.text),
          ),
          const SizedBox(height: AppSpacing.x8l),
          AppField(
            controller: _controller,
            shape: AppFieldShape.hero,
            autofocus: true,
            hint: 'kanpachi.ejemplo.com',
            onSubmitted: (_) => _guardar(),
          ),
          const SizedBox(height: AppSpacing.lg),
          if (_error != null)
            Text(
              _error!,
              textAlign: TextAlign.center,
              style: context.type.bodySm.copyWith(color: colors.danger),
            )
          else if (sugerido)
            Text(
              'Este es el servidor de la última sala en la que estuviste. '
              'Cámbialo si no es el tuyo.',
              textAlign: TextAlign.center,
              style: context.type.bodySm.copyWith(color: colors.textMuted),
            )
          else
            Text(
              'El nombre del servidor, sin https:// y sin barras',
              style: context.type.bodySm.copyWith(
                color: colors.textMuted,
                height: 1,
              ),
            ),
          const SizedBox(height: AppSpacing.x7l),
          const AppExplainer(
            'Es el punto de encuentro de tus salas: ve tu IP pública y por él '
            'pasa todo el que entre con tu código. Ponlo solo si confías en '
            'quien lo administra.',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: AppSpacing.x8l),
          AppButton(
            label: 'Guardar',
            width: 220,
            onPressed: _valido ? _guardar : null,
          ),
        ],
      ),
    );
  }
}
