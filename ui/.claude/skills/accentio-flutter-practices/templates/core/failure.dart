// lib/core/failures/failure.dart
//
// Errores tipados que lanzan las capas data/domain. Los cubits los atrapan en
// onError y los mapean a copy. NUNCA se swallowean.

import 'app_error_code.dart';

abstract class Failure implements Exception {}

/// Lleva el código numérico del backend. Se mezcla en toda Failure que pueda
/// originarse en un status auth-aware (400/401/403/409), así los cubits hacen
/// `if (e is AuthCoded) appErrorMessage(e.code)` en vez de switchear subtipos.
mixin AuthCoded on Failure {
  int? get code;
}

class CommonFailure extends Failure {
  CommonFailure([this.message]);
  final String? message;
}

class NetworkFailure extends Failure {}

class NotFoundFailure extends Failure {}

class JsonParseFailure extends Failure {
  JsonParseFailure({required this.error});
  final Object error;
}

class UnauthorizedFailure extends Failure with AuthCoded {
  UnauthorizedFailure([this.code]);
  @override
  final int? code;
}

class ForbiddenFailure extends Failure with AuthCoded {
  ForbiddenFailure([this.code]);
  @override
  final int? code;
}

class BadRequestFailure extends CommonFailure with AuthCoded {
  BadRequestFailure([super.message, this.code]);
  @override
  final int? code;
}

class ConflictFailure extends CommonFailure with AuthCoded {
  ConflictFailure([super.message, this.code]);
  @override
  final int? code;
}

/// Ejemplo de subclase con METADATA: la UI necesita campos estructurados y no
/// debería re-parsear el body. Se construye en `checkFailures` desde `data`.
class DataConflictFailure extends ConflictFailure {
  DataConflictFailure({required this.serverVersion, String? message})
    : super(message ?? 'Data conflict', AppErrorCode.dataConflict);

  final int? serverVersion;
}
