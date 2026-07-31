/// Hierarchy of typed errors thrown by the data/domain layers. Cubits catch
/// these and map to user-facing messages — never swallow.
abstract class Failure implements Exception {}

mixin AuthCoded on Failure {
  int? get code;
}

class CommonFailure extends Failure {
  CommonFailure([this.message]);
  final String? message;

  @override
  String toString() => 'CommonFailure(${message ?? ''})';
}

class NetworkFailure extends Failure {
  @override
  String toString() => 'NetworkFailure';
}

/// 401 after the auth interceptor's refresh attempt failed (or after the
/// interceptor decided not to refresh because the [code] said it wouldn't
/// help — see `AppErrorCode.refreshable`).
class UnauthorizedFailure extends Failure with AuthCoded {
  UnauthorizedFailure([this.code]);
  @override
  final int? code;

  @override
  String toString() => 'UnauthorizedFailure(${code ?? '-'})';
}

/// 403 — caller is authenticated but lacks the required permission/scope.
class ForbiddenFailure extends Failure with AuthCoded {
  ForbiddenFailure([this.code]);
  @override
  final int? code;

  @override
  String toString() => 'ForbiddenFailure(${code ?? '-'})';
}

class BadRequestFailure extends CommonFailure with AuthCoded {
  BadRequestFailure([super.message, this.code]);
  @override
  final int? code;

  @override
  String toString() =>
      'BadRequestFailure(${code ?? '-'}${message == null ? '' : ' · $message'})';
}

class NotFoundFailure extends Failure {
  @override
  String toString() => 'NotFoundFailure';
}

/// 429 — rate-limited by the server throttle.
class ThrottledFailure extends Failure with AuthCoded {
  ThrottledFailure([this.code]);
  @override
  final int? code;

  @override
  String toString() => 'ThrottledFailure(${code ?? '-'})';
}