import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/features/session/domain/entities/engine_info.dart';
import 'package:kanpachi_ui/features/session/domain/repositories/session_repository.dart';
import 'package:kanpachi_ui/ioc/injector.dart';
import 'package:kanpachi_ui/core/brand.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/core/platform/app_version.dart';
import 'package:kanpachi_ui/core/platform/system_browser.dart';
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

  /// Qué motor lleva esta instalación, contestado por el daemon una vez al
  /// entrar. Vacío mientras contesta, si el daemon no está, o con un motor
  /// anterior al centinela: en los tres casos la línea no se pinta, porque
  /// una versión inventada sería peor que ninguna.
  EngineInfo _motor = const EngineInfo();

  @override
  void initState() {
    super.initState();
    unawaited(_leerMotor());
  }

  Future<void> _leerMotor() async {
    try {
      final EngineInfo info = await Injector.instance
          .get<SessionRepository>()
          .engineInfo();
      if (mounted) setState(() => _motor = info);
    } on Object {
      // Sin daemon no hay a quién preguntarle, y esta tarjeta sigue diciendo
      // su versión propia: el detalle del motor es un extra, no una condición.
    }
  }

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
      padding: AppSpacing.cardInset,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          const AppKicker('Verificar actualizaciones'),
          const SizedBox(height: AppSpacing.x5l),
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    // La versión que corre, nombrada. «Tienes la 0.2.1» decía
                    // lo mismo con una frase donde alcanza un rótulo, y encima
                    // dejaba la línea de abajo repitiendo el verbo.
                    // Una versión para las tres caras, y decirlo es parte del
                    // dato: el daemon, la terminal y esta ventana salen del
                    // mismo corte y viajan juntos.
                    Text(
                      'Kanpachi $kAppVersion (daemon, terminal y ventana)',
                      style: context.type.body.copyWith(color: colors.text),
                    ),
                    // El motor va aparte porque ES aparte: se versiona por su
                    // tag y viaja fijado por engine.pin. La línea dice el
                    // build id sellado en el binario y la librería de red que
                    // lleva dentro, que es lo que hace contestable «¿qué motor
                    // es este?» sin abrir una terminal.
                    if (_motor.known) ...<Widget>[
                      const SizedBox(height: 2),
                      Text(
                        'Motor $_motor',
                        style: context.type.bodySm.copyWith(
                          color: colors.textMuted,
                        ),
                      ),
                    ],
                    const SizedBox(height: 4),
                    Text(
                      switch ((nueva, _preguntado)) {
                        // Lleva el número: «hay una nueva» es una afirmación
                        // que nadie puede comprobar, y «la 0.2.2 está fuera»
                        // se compara contra lo que diga la página.
                        (final String v?, _) =>
                          'Hay una actualización nueva, la $v. '
                              'Pulsa para bajarla.',
                        (null, true) =>
                          'Tienes la versión más nueva hasta ahora. Nada que '
                              'hacer por acá.',
                        (null, false) =>
                          'Verifica si hay nuevas actualizaciones de Kanpachi.',
                      },
                      style: context.type.bodySm.copyWith(
                        color: nueva == null ? colors.textMuted : colors.accent,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: AppSpacing.xl),
              // Sabiendo que hay una nueva, preguntar otra vez no contesta
              // nada: la respuesta ya está y no cambia. El botón pasa a llevar
              // a donde se baja, que es lo único que queda por hacer.
              //
              // **Lleva a la página, y no descarga ni instala nada.** Kanpachi
              // no se actualiza solo, ver `docs/07-futuro.md`: lo que haría un
              // botón de «Actualizar» es un servicio corriendo como SYSTEM
              // reemplazando su binario por una descarga sin firmar.
              AppButton(
                label: switch ((nueva, _buscando)) {
                  (_, true) => 'Buscando...',
                  (final String _?, _) => 'Descargar',
                  _ => 'Buscar',
                },
                onPressed: switch ((nueva, _buscando)) {
                  (_, true) => null,
                  (final String _?, _) => () => SystemBrowser.open(
                    Brand.releases,
                  ),
                  _ => _buscar,
                },
              ),
            ],
          ),
        ],
      ),
    );
  }
}
