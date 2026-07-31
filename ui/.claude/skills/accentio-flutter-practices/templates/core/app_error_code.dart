// lib/core/failures/app_error_code.dart
//
// Espejo del enum de códigos del backend (p. ej. src/core/error-codes.ts).
// El wire SIEMPRE lleva {statusCode, code:<int>, message, data?, timestamp,
// path}. El cliente switchea el `code`, NUNCA el `message`.

abstract final class AppErrorCode {
  // Bandas: 1xxx auth · 2xxx permisos · 3xxx validación · 4xxx dominio/sync ·
  //         9xxx genérico.

  // ── 1xxx auth ─────────────────────────────────────────────────────────────
  static const int loginFailed = 1001;
  static const int tokenExpired = 1010;
  static const int tokenInvalid = 1011;
  static const int noAuth = 1020;
  static const int userGone = 1021;
  static const int refreshReused = 1032;

  // ── 2xxx permisos ─────────────────────────────────────────────────────────
  static const int permissionDenied = 2001;

  // ── 4xxx dominio ──────────────────────────────────────────────────────────
  static const int dataConflict = 4001;

  // ── 9xxx genérico ─────────────────────────────────────────────────────────
  static const int unknown = 9000;

  /// Códigos que significan "tu *access* token es el problema; probá
  /// /auth/refresh antes de mandar a login".
  ///
  /// Cualquier otro 401 sale directo como UnauthorizedFailure: no quemar un
  /// refresh donde no puede ayudar (sin token, usuario borrado, refresh
  /// reusado).
  static const Set<int> refreshable = {tokenExpired, tokenInvalid};
}

// lib/core/failures/app_error_messages.dart
//
// Un case por código. Devolver null para "no tengo copy" → el caller usa su
// fallback. También null a propósito para los códigos que un cubit intercepta
// ANTES del mapeo genérico (porque enrutan a una pantalla dedicada).
String? appErrorMessage(int? code) {
  if (code == null) return null;
  switch (code) {
    case AppErrorCode.loginFailed:
      return 'Credenciales inválidas';
    case AppErrorCode.tokenExpired:
    case AppErrorCode.tokenInvalid:
      return 'Sesión expirada. Iniciá sesión otra vez.';
    case AppErrorCode.permissionDenied:
      return 'No tenés permiso para esta acción.';
  }
  return null;
}
