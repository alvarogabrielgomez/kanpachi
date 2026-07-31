// lib/integrations/packages/dio/api.dart

import '../../../ioc/injector.dart';
import '../../environments/remote_service.dart';
import '../../helpers/http_helper.dart';
import 'dio_http_helper.dart';

/// Cómo un repositorio remoto obtiene su [HttpHelper].
///
/// Un getter por microservicio. Uso: `class XRemoteRepository with Api`.
mixin Api {
  HttpHelper get appApi => Injector.instance
      .resolve<DioHttpHelper>()
      .withBaseUrl(RemoteService.appApi);
}
