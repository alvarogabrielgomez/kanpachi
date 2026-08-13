import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/brand.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/platform/app_version.dart';
import 'package:kanpachi_ui/features/update/presentation/cubit/update_cubit.dart';

/// Buscar versión nueva, a pedido.
///
/// # Por qué hay un botón, si antes no hacía falta
///
/// Porque la comprobación dejó de ser automática. Preguntaba sola al arrancar y
/// en cada sala que se abría o se cerraba, y eso valía mientras la pregunta iba
/// al registro, que era uno solo, compilado, y tenía la respuesta cacheada para
/// todos. Ahora va al canal de publicación, y una consulta por sesión y por
/// máquina no cabe en la cuota de sesenta por hora y por IP que comparte toda
/// una casa. A pedido, quien pulsa gasta una de sesenta.
///
/// Lo que se sabe SÍ sobrevive sin preguntar: vive en disco y el aviso de abajo
/// reaparece solo. Este botón es para enterarse antes, no para volver a ver.
class UpdateCheckCard extends StatefulWidget {
  const UpdateCheckCard({super.key});

  @override
  State<UpdateCheckCard> createState() => _UpdateCheckCardState();
}

class _UpdateCheckCardState extends State<UpdateCheckCard> {
  bool _buscando = false;

  /// Si ya se preguntó en esta visita. Cambia el texto de «no se sabe» a «no
  /// hay nada nuevo», que son dos cosas distintas y se veían iguales.
  bool _preguntado = false;

  Future<void> _buscar() async {
    setState(() => _buscando = true);
    await context.read<UpdateCubit>().check(force: true);
    if (!mounted) return;
    setState(() {
      _buscando = false;
      _preguntado = true;
    });
  }

  @override
  Widget build(BuildContext context) {
    // Un fork que publica por otro canal apaga la comprobación entera, y
    // entonces esta tarjeta no existe: un botón que no puede contestar es peor
    // que ninguno. Ver [Brand.updatesEnabled].
    if (!Brand.updatesEnabled) return const SizedBox.shrink();

    final colors = context.colors;
    final String? nueva = context.select<UpdateCubit, String?>(
      (UpdateCubit c) => c.state.available,
    );

    return AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          const AppKicker('Versión'),
          const SizedBox(height: AppSpacing.x5l),
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      'Tienes la $kAppVersion',
                      style: context.type.body.copyWith(color: colors.text),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      switch ((nueva, _preguntado)) {
                        (final String v?, _) => 'Hay una más nueva: $v',
                        (null, true) => 'No hay ninguna más nueva.',
                        (null, false) =>
                          'Kanpachi no consulta solo. Pulsa para preguntar.',
                      },
                      style: context.type.bodySm.copyWith(
                        color: nueva == null ? colors.textMuted : colors.accent,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: AppSpacing.xl),
              AppButton(
                label: _buscando ? 'Buscando...' : 'Buscar',
                onPressed: _buscando ? null : _buscar,
              ),
            ],
          ),
        ],
      ),
    );
  }
}
