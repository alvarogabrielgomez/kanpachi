// lib/integrations/packages/alice/alice_debug_inspector.dart

import 'package:alice_lightweight/alice.dart';
import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';

/// Inspector HTTP debug-only. En release [attach] es no-op y las clases de
/// Alice se tree-shakean.
///
/// Se engancha al MISMO Dio que usa el resto de la app, así que cada request
/// (auth, sync, lo que sea) aparece en el inspector. Una notificación sticky de
/// prioridad baja muestra el contador de llamadas; el tap abre el inspector
/// dentro de la app corriendo, vía [navigatorKey].
///
/// `alice_lightweight` v3 sacó la notificación built-in, así que la hacemos con
/// `flutter_local_notifications`.
class AliceDebugInspector {
  AliceDebugInspector._({
    required Alice alice,
    required FlutterLocalNotificationsPlugin notifications,
  }) : _alice = alice,
       _notifications = notifications;

  final Alice _alice;
  final FlutterLocalNotificationsPlugin _notifications;

  static const _channelId = 'alice_inspector';
  static const _channelName = 'HTTP inspector (debug)';
  static const _notificationId = 8888;

  static AliceDebugInspector? _instance;
  static AliceDebugInspector? get instance => _instance;

  int _calls = 0;

  /// Engancha Alice a [dio]. Devuelve null en release.
  ///
  /// [registerTapHandler] rutea el tap de la notificación de vuelta a [_open]
  /// a través del ÚNICO dueño del plugin de notificaciones. FLN admite un solo
  /// `onDidReceiveNotificationResponse` global: re-llamar `initialize()` acá
  /// clobbearía los callbacks del dueño — comparten el mismo singleton de
  /// proceso.
  static Future<AliceDebugInspector?> attach({
    required Dio dio,
    required GlobalKey<NavigatorState> navigatorKey,
    void Function(int notificationId, void Function() onTap)? registerTapHandler,
  }) async {
    if (!kDebugMode) return null;
    if (_instance != null) return _instance; // idempotente

    final alice = Alice(navigatorKey: navigatorKey);
    // Reusar el plugin ya inicializado por su dueño. NO llamar initialize().
    // El show() de abajo crea el canal implícitamente en el primer render.
    final notifications = FlutterLocalNotificationsPlugin();

    final inspector = AliceDebugInspector._(
      alice: alice,
      notifications: notifications,
    );
    _instance = inspector;
    registerTapHandler?.call(_notificationId, () => _instance?._open());

    // Orden: el de Alice PRIMERO, para que capture todo.
    dio.interceptors.add(alice.getDioInterceptor());
    dio.interceptors.add(_NotificationBumpInterceptor(inspector));

    await inspector._render();
    return inspector;
  }

  /// Abrir el inspector a mano (p. ej. desde una pantalla de debug).
  void show() => _open();

  void _open() => _alice.showInspector();

  Future<void> _bump() async {
    _calls += 1;
    await _render();
  }

  Future<void> _render() async {
    await _notifications.show(
      id: _notificationId,
      title: 'HTTP inspector',
      body: _calls == 0
          ? 'Tap to open · sin requests'
          : 'Tap to open · $_calls request${_calls == 1 ? '' : 's'}',
      notificationDetails: const NotificationDetails(
        android: AndroidNotificationDetails(
          _channelId,
          _channelName,
          channelDescription: 'Debug inspector for HTTP traffic',
          importance: Importance.low,
          priority: Priority.low,
          ongoing: true,
          autoCancel: false,
          showWhen: false,
          playSound: false,
          enableVibration: false,
          category: AndroidNotificationCategory.service,
        ),
        iOS: DarwinNotificationDetails(presentBadge: false, presentSound: false),
      ),
    );
  }
}

class _NotificationBumpInterceptor extends Interceptor {
  _NotificationBumpInterceptor(this._inspector);

  final AliceDebugInspector _inspector;

  @override
  void onResponse(Response response, ResponseInterceptorHandler handler) {
    _inspector._bump();
    handler.next(response);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) {
    _inspector._bump();
    handler.next(err);
  }
}

// ── Wiring en main(), después de que el IoC y el navigatorKey existan ───────
//
// if (kDebugMode) {
//   await AliceDebugInspector.attach(
//     dio: Injector.instance.resolve<DioHttpHelper>().dio,
//     navigatorKey: rootNavigatorKey,
//     registerTapHandler: notificationsOwner.registerForeignTapHandler,
//   );
// }
