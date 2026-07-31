// Plantilla de una feature completa. En el proyecto real cada bloque va en su
// archivo, en las rutas que indica el encabezado.
//
//   features/profile/
//   ├── domain/entities/profile_entity.dart
//   ├── domain/repositories/profile_repository.dart
//   ├── domain/use_cases/profile_use_cases.dart  (+ _impl.dart)
//   ├── data/dto/profile_response_dto.dart
//   ├── data/repositories/profile_remote_repository.dart
//   └── presentation/cubit/profile_cubit.dart (+ profile_state.dart)

import 'package:flutter_bloc/flutter_bloc.dart';

import '../../core/failures/app_error_code.dart';
import '../../core/failures/failure.dart';
import '../../integrations/helpers/http_helper.dart';
import '../../integrations/packages/dio/api.dart';

// ── domain/entities/profile_entity.dart ─────────────────────────────────────
// La clase de negocio. Si además es la fila de la DB local, acá van las
// anotaciones de ObjectBox (@Entity) — no hay DTO de persistencia aparte.

class ProfileEntity {
  const ProfileEntity({required this.id, required this.displayName});
  final String id;
  final String displayName;
}

// ── data/dto/profile_response_dto.dart ──────────────────────────────────────
// El shape del WIRE. No se contagia al dominio.

class ProfileResponseDto {
  const ProfileResponseDto({required this.id, required this.name});

  factory ProfileResponseDto.fromJson(Map<String, dynamic> json) =>
      ProfileResponseDto(
        id: json['id'] as String,
        name: json['name'] as String? ?? '',
      );

  final String id;
  final String name;
}

class UpdateProfileRequestDto {
  const UpdateProfileRequestDto({required this.name});
  final String name;

  Map<String, dynamic> toJson() => {'name': name};
}

// ── domain/repositories/profile_repository.dart ─────────────────────────────
// Contrato wire-level. Devuelve Stream<DTO> por convención; los consumidores
// (use cases) transforman DTO → Entity.

abstract class ProfileRepository {
  Stream<ProfileResponseDto> me();
  Stream<ProfileResponseDto> update(UpdateProfileRequestDto req);
  Stream<void> deleteAvatar();
}

// ── data/repositories/profile_remote_repository.dart ────────────────────────

class ProfileRemoteRepository with Api implements ProfileRepository {
  static const _me = '/profile/me';
  static const _avatar = '/profile/avatar';

  @override
  Stream<ProfileResponseDto> me() async* {
    yield* appApi.get(_me).handle<ProfileResponseDto>(
      mapper: (r) => ProfileResponseDto.fromJson(r as Map<String, dynamic>),
    );
  }

  @override
  Stream<ProfileResponseDto> update(UpdateProfileRequestDto req) async* {
    yield* appApi.patch(_me, data: req.toJson()).handle<ProfileResponseDto>(
      mapper: (r) => ProfileResponseDto.fromJson(r as Map<String, dynamic>),
    );
  }

  @override
  Stream<void> deleteAvatar() async* {
    yield* appApi.delete(_avatar).handleVoid(); // 204 → handleVoid
  }
}

// ── domain/use_cases/profile_use_cases.dart (+ _impl) ───────────────────────
// Drena el stream con .first, transforma DTO→Entity, aplica reglas.
// Los Failure suben SIN atrapar.

abstract class ProfileUseCases {
  Stream<ProfileEntity> load();
  Stream<ProfileEntity> rename(String name);
}

class ProfileUseCasesImpl implements ProfileUseCases {
  ProfileUseCasesImpl({required this.repository});

  final ProfileRepository repository;

  @override
  Stream<ProfileEntity> load() async* {
    final dto = await repository.me().first;
    yield _toEntity(dto);
  }

  @override
  Stream<ProfileEntity> rename(String name) async* {
    final trimmed = name.trim();
    final dto = await repository
        .update(UpdateProfileRequestDto(name: trimmed))
        .first;
    yield _toEntity(dto);
  }

  ProfileEntity _toEntity(ProfileResponseDto dto) =>
      ProfileEntity(id: dto.id, displayName: dto.name);
}

// ── presentation/cubit/profile_cubit.dart ───────────────────────────────────
// El ÚNICO lugar donde un Failure se convierte en copy.

sealed class ProfileState {
  const ProfileState();
}

class ProfileInitial extends ProfileState {
  const ProfileInitial();
}

class ProfileLoading extends ProfileState {
  const ProfileLoading();
}

class ProfileReady extends ProfileState {
  const ProfileReady(this.profile);
  final ProfileEntity profile;
}

class ProfileError extends ProfileState {
  const ProfileError(this.message);
  final String message;
}

class ProfileCubit extends Cubit<ProfileState> {
  ProfileCubit(this._uc) : super(const ProfileInitial());

  final ProfileUseCases _uc;

  void load() {
    emit(const ProfileLoading());
    _uc.load().listen(
      (p) => emit(ProfileReady(p)),
      onError: (Object e) => emit(ProfileError(_mapError(e))),
    );
  }

  /// Orden de chequeo: red → código de app → mensaje → fallback.
  String _mapError(Object e) {
    if (e is NetworkFailure) return 'Sin conexión a internet';
    if (e is AuthCoded) {
      final mapped = appErrorMessage(e.code);
      if (mapped != null) return mapped;
    }
    if (e is CommonFailure) return e.message ?? 'Error desconocido';
    return 'Error desconocido';
  }
}
