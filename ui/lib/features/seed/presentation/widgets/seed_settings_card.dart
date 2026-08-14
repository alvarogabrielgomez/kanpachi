import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';

/// En qué servidor abre sus salas esta máquina, con su forma de cambiarlo.
///
/// # Por qué esto faltaba, y por qué es un agujero y no un adorno
///
/// Porque desde que el servidor dejó de venir de fábrica es el único ajuste que
/// el producto tiene de verdad, y a la pantalla de elegirlo **solo se llegaba
/// fallando**: intentando abrir una sala sin ninguno configurado, o desde el
/// diálogo de confianza. Configuración, que se presenta como «lo poco que hay
/// que decidir», no lo enseñaba ni lo dejaba cambiar, mientras la barra de
/// estado de esa misma ventana decía «sin servidor».
///
/// # Por qué enseña el nombre y no solo un botón
///
/// Porque la pregunta que trae a alguien acá casi siempre es «¿en cuál estoy?»,
/// y contestarla exige leerlo. Va en monoespaciada por lo mismo que en el
/// diálogo de confianza: es un nombre que se COMPARA carácter a carácter contra
/// el que le mandó otra persona.
///
/// # Lo que NO hay acá, a propósito
///
/// El password. Se pide cuando abrir una sala falla porque el servidor lo pide,
/// y en ningún otro momento: casi ningún servidor lo pide, y ofrecerlo acá le
/// cobraría a todo el mundo el roce de un caso raro. Ver la pantalla E de
/// `docs/05-ui.md`.
class SeedSettingsCard extends StatefulWidget {
  const SeedSettingsCard({super.key});

  @override
  State<SeedSettingsCard> createState() => _SeedSettingsCardState();
}

class _SeedSettingsCardState extends State<SeedSettingsCard> {
  @override
  void initState() {
    super.initState();
    // Se relee al entrar. El servidor no viaja en el latido, así que lo que
    // haya en el estado puede ser de hace una hora, y esta pantalla es
    // justamente donde alguien viene a verlo.
    //
    unawaited(_releer(context.read<SessionCubit>()));
  }

  /// Relee el servidor y deja el fallo en el log, no en pantalla.
  ///
  /// Lo que se pierde cuando falla es refrescar un dato que ya está dibujado, y
  /// una tarjeta de error acá taparía los dos ajustes de al lado por algo que se
  /// cura volviendo a entrar. Se registra en vez de tragarse en silencio: un
  /// `catch` vacío es cómo un fallo repetido no se descubre nunca.
  Future<void> _releer(SessionCubit session) async {
    try {
      await session.ownSeed();
    } on Object catch (e) {
      debugPrint('no se pudo releer el servidor configurado: $e');
    }
  }

  @override
  Widget build(BuildContext context) {
    final colors = context.colors;
    final String seed = context.select<SessionCubit, String>(
      (SessionCubit c) => c.state.ownSeed,
    );
    final bool hay = seed.isNotEmpty;

    return AppCard(
      padding: AppSpacing.cardInset,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          const AppKicker('Servidor de encuentro'),
          const SizedBox(height: AppSpacing.x5l),
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      hay ? seed : 'Todavía no elegiste ninguno',
                      style: hay
                          ? context.type.mono.copyWith(color: colors.text)
                          : context.type.body.copyWith(color: colors.text),
                    ),
                    const SizedBox(height: 4),
                    Text(
                      hay
                          ? 'Presenta entre sí a los que entran con tu código, '
                                'hasta que la sala se levanta. Desde ahí el '
                                'juego va directo entre ustedes, salvo la '
                                'conexión que no pueda y tenga que pasar por él.'
                          : 'Hace falta solo para ABRIR salas. Entrar a la de '
                                'otra persona no lo usa y no pide nada.',
                      style: context.type.bodySm.copyWith(
                        color: colors.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: AppSpacing.xl),
              AppButton(
                label: hay ? 'Cambiar' : 'Elegir',
                onPressed: () => context.read<ShellCubit>().go(AppScreen.seed),
              ),
            ],
          ),
        ],
      ),
    );
  }
}
