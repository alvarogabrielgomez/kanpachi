import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_bloc/flutter_bloc.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_icon_button.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_kicker.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_switch.dart';
import 'package:kanpachi_ui/core/design_system/atoms/app_card.dart';
import 'package:kanpachi_ui/core/design_system/tokens/context_ext.dart';
import 'package:kanpachi_ui/core/design_system/tokens/spacing_tokens.dart';
import 'package:kanpachi_ui/features/seed/presentation/widgets/seed_settings_card.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_cubit.dart';
import 'package:kanpachi_ui/features/update/presentation/widgets/update_check_card.dart';
import 'package:kanpachi_ui/features/session/presentation/cubit/session_state.dart';
import 'package:kanpachi_ui/features/shell/presentation/cubit/shell_cubit.dart';
import 'package:kanpachi_ui/features/shell/presentation/widgets/screen_frame.dart';

/// Los ajustes: los pocos que hay, y ninguno inventado.
///
/// # Por qué está escondida detrás de un engranaje
///
/// Porque nada de acá hace falta para jugar. Kanpachi promete que se instala y
/// funciona, y una pantalla de ajustes en la portada dice lo contrario: que hay
/// algo que configurar antes de empezar. Estos dos existen para la persona que
/// tiene un problema concreto, y esa persona sabe buscar.
///
/// # Por qué son estos y no más
///
/// El resto de lo que se podría poner acá ya lo decide alguien mejor: el
/// firewall lo manda el daemon, el juego lo eligen dentro de la sala, y el
/// nombre tiene su propia pantalla porque se pide en el alta. Lo que queda son
/// las cosas que el usuario sí tiene derecho a cambiar y nadie más puede
/// decidir por él.
///
/// # El servidor faltaba acá, y era un agujero
///
/// Desde que dejó de venir de fábrica es el único ajuste que decide algo del
/// PRODUCTO y no de la app, y a la pantalla de elegirlo solo se llegaba
/// fallando: intentando abrir una sala sin ninguno, o desde el diálogo de
/// confianza. Mientras tanto la barra de estado de esta misma ventana decía
/// «sin servidor» y desde acá no había forma de contestarle. Ver
/// [SeedSettingsCard].
class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  /// Si Kanpachi arranca con Windows, tal como lo contestó el sistema.
  ///
  /// Null es "todavía no se sabe", que es un estado de verdad y no un hueco:
  /// hay que preguntárselo al daemon, y la pregunta puede fallar. Se distingue
  /// de `false` a propósito, porque dibujar el interruptor apagado mientras no
  /// se sabe es afirmar algo que nadie midió.
  bool? _autostart;

  /// Por qué no se pudo leer o escribir, si es el caso.
  String? _fallo;

  /// Mientras el daemon contesta. Apaga el interruptor para que un doble clic
  /// no mande dos cambios de configuración del servicio.
  bool _ocupado = true;

  /// Si esto es una copia portable, en cuyo caso el arranque con Windows NO
  /// EXISTE como pregunta.
  ///
  /// No es un ajuste escondido ni uno deshabilitado: en una carpeta portable no
  /// hay servicio registrado, así que no hay nada que Windows pueda levantar al
  /// encender. Un interruptor apagado y gris diría que la opción existe y que
  /// hoy no se puede, que son dos cosas falsas.
  ///
  /// Sale del marcador en disco y no de una bandera de compilación, igual que
  /// el pipe y el directorio de datos: así un solo `kanpachiui.exe` sirve para
  /// el producto instalado y para el bundle portable, sin dos builds que se
  /// puedan cruzar.
  ///
  /// **Llega por el estado del marco y no se lee acá.** Quien conoce el
  /// marcador es `PipeNames`, que vive en infra, y una pantalla que importa
  /// infra rompe el candado de `import_purity_test`. Lo pregunta `main()`, que
  /// ya lo necesitaba para otra cosa, y baja por el registro de dependencias.
  /// Ver [ShellState.portable].
  late final bool _portable;

  @override
  void initState() {
    super.initState();
    // `read` y no `watch`: es un hecho de la máquina que no cambia mientras la
    // ventana vive, así que no hay a qué suscribirse.
    _portable = context.read<ShellCubit>().state.portable;
    // En portable no se pregunta nada: no hay servicio del que leer el
    // arranque, y preguntarlo dejaría un fallo en pantalla que no es un fallo.
    if (_portable) {
      _ocupado = false;
      return;
    }
    // Se pregunta al ENTRAR y no se guarda en ningún sitio: el dueño de esta
    // respuesta es el Administrador de servicios, y entre una visita y la
    // siguiente pudo cambiarla cualquiera desde services.msc.
    unawaited(_preguntar());
  }

  /// Lee, y opcionalmente cambia, el arranque con Windows.
  ///
  /// Es una sola llamada para las dos cosas porque el daemon lo expone así, y
  /// el motivo es este: lo que vuelve es lo que quedó puesto DE VERDAD,
  /// releído del Administrador de servicios. Pintar lo que se pidió enseñaría
  /// como aceptado un cambio que Windows pudo rechazar.
  Future<void> _preguntar({bool? enabled}) async {
    setState(() => _ocupado = true);
    try {
      final bool puesto = await context.read<SessionCubit>().autostart(
        enabled: enabled,
      );
      if (!mounted) return;
      setState(() {
        _autostart = puesto;
        _fallo = null;
      });
    } on Object catch (e) {
      if (!mounted) return;
      // Se deja el interruptor en lo último que sí se supo, con el motivo
      // debajo. Ponerlo en falso sería contestar la pregunta con una
      // suposición justo en la pantalla que existe para contestarla.
      setState(() => _fallo = e.toString());
    } finally {
      if (mounted) setState(() => _ocupado = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final SessionState session = context.watch<SessionCubit>().state;
    final colors = context.colors;

    return ScreenBody(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ScreenHeader(
            title: 'Configuración',
            note:
                'Lo poco que hay que decidir. Kanpachi funciona sin tocar nada '
                'de acá.',
            // **Vuelve a donde se estaba**, que ya no se adivina acá.
            //
            // Este destino estuvo escrito a mano dos veces y las dos se quedó
            // corto. Primero era siempre la portada, y eso dejaba la app
            // bloqueada: se salía de la sala en la pantalla mientras el daemon
            // seguía dentro, así que crear otra contestaba que ya estabas en
            // una. Después fue «la sala si hay sala», que acierta viniendo de
            // la sala y falla viniendo del selector de juego o de un enlace.
            // Adivinar el origen desde el destino no tiene arreglo: lo sabe el
            // historial, y ahora hay uno.
            leading: AppBackButton(
              onPressed: () => context.read<ShellCubit>().back(),
            ),
          ),
          const SizedBox(height: AppSpacing.x7l),
          // La tarjeta ENTERA, y no solo el interruptor: en portable «Este
          // equipo» se quedaría con un título y nada debajo. Ver [_portable].
          if (!_portable) ...<Widget>[
            AppCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  const AppKicker('Este equipo'),
                  const SizedBox(height: AppSpacing.x5l),
                  AppSwitchRow(
                    title: 'Arrancar Kanpachi con Windows',
                    note: _notaArranque,
                    value: _autostart ?? false,
                    enabled: !_ocupado && _autostart != null,
                    onChanged: (bool v) => _preguntar(enabled: v),
                  ),
                  if (_fallo != null) ...<Widget>[
                    const SizedBox(height: AppSpacing.xl),
                    SelectableText(
                      _fallo!,
                      style: context.type.monoSm.copyWith(color: colors.warn),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: AppSpacing.x4l),
          ],
          // El servidor va PRIMERO de los que quedan, y no al final: es el
          // único ajuste que decide algo del producto, y los otros dos son
          // sobre la app. En una copia portable es además el primero de todos,
          // porque «Este equipo» no existe ahí.
          const SeedSettingsCard(),
          const SizedBox(height: AppSpacing.x4l),
          const UpdateCheckCard(),
          const SizedBox(height: AppSpacing.x4l),
          AppCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                const AppKicker('Diagnóstico'),
                const SizedBox(height: AppSpacing.x5l),
                AppSwitchRow(
                  title: 'Contar paso a paso lo que hace el servicio',
                  note:
                      'Enseña, mientras se abre una sala, cada paso con su '
                      'reloj: el rango elegido, el código que dio el registro, '
                      'el motor arrancando, el adaptador tomando su dirección. '
                      'Si algo falla, esos pasos quedan dentro de «ver '
                      'detalles» del error. Cuesta una consulta al servicio '
                      'un par de veces por segundo mientras dura la espera, y '
                      'nada el resto del tiempo.',
                  value: session.verbose,
                  onChanged: (bool v) =>
                      context.read<SessionCubit>().setVerbose(enabled: v),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// Lo que dice debajo del interruptor de arranque, que cambia con lo que se
  /// sabe.
  ///
  /// **Apagarlo NO desinstala nada**, y hay que decirlo ahí mismo: es lo
  /// primero que alguien teme al ver este interruptor, y el miedo a
  /// desinstalar por accidente es lo que hace que no lo toque quien lo
  /// necesita.
  String get _notaArranque {
    if (_autostart == null) {
      return 'Todavía no se pudo preguntar. El servicio sigue como esté; esto '
          'solo no se ha podido leer.';
    }
    return 'Apagarlo no quita nada del sistema: el servicio sigue instalado y '
        'Kanpachi arranca igual cuando lo abres. Lo único que cambia es que '
        'Windows deja de levantarlo solo al encender.';
  }
}
