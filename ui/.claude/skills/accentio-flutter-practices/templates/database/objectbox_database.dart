// lib/core/database/objectbox_database.dart
//
// ObjectBox es la DB local de la casa (Android · iOS · Windows · macOS · Linux).
// Guarda ENTIDADES DE DOMINIO directo — no hay capa de DTOs de persistencia.

import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

// Generado por `dart run build_runner build`. Trae openStore() y los Box<T>.
import '../../objectbox.g.dart';

/// Dueño del `Store` y de los accessors de cajas.
///
/// Se crea UNA vez en `main()` (antes del IoC) y se registra como singleton:
/// abrir dos stores sobre el mismo directorio tira
/// `Cannot open store: another store is still open`.
class ObjectBoxDatabase {
  ObjectBoxDatabase._(this.store);

  final Store store;

  static Future<ObjectBoxDatabase> create() async {
    final docs = await getApplicationDocumentsDirectory();
    final store = await openStore(
      directory: p.join(docs.path, 'app-db'),
      // macOS/iOS sandbox: si algún día hay App Group, va acá.
      // macosApplicationGroup: 'XXXXXX.app-group',
    );
    return ObjectBoxDatabase._(store);
  }

  // Un getter por caja. Nunca `store.box<X>()` suelto en una feature: el
  // accessor es el punto donde se ve, de un vistazo, todo lo que persiste.
  Box<ProfileEntity> get profiles => store.box<ProfileEntity>();

  void close() => store.close();
}

// ── La entidad: dominio + persistencia en la misma clase ────────────────────
//
// import 'package:objectbox/objectbox.dart';
//
// @Entity()
// class ProfileEntity {
//   ProfileEntity({this.id = 0, required this.displayName});
//
//   /// `int id` con default 0 = "todavía no persistida". Lo asigna ObjectBox
//   /// en el put(). NO es la clave de negocio.
//   @Id()
//   int id;
//
//   /// La clave ESTABLE entre dispositivos/backups: un UUID que asigna la app.
//   /// El `id` de ObjectBox es local al store y se reasigna en un restore, así
//   /// que nunca puede ser la referencia cruzada de un payload.
//   @Unique(onConflict: ConflictStrategy.replace)
//   String clientId = '';
//
//   String displayName;
//
//   @Property(type: PropertyType.date)
//   DateTime? updatedAt;
//
//   /// Los enums se persisten por su ÍNDICE: es contrato: reordenar los
//   /// valores reinterpreta la data ya escrita. Agregar al final, nunca en el
//   /// medio, y exponer el enum por un getter/setter sobre la columna int.
//   int dbKind = 0;
//   ProfileKind get kind => ProfileKind.values[dbKind];
//   set kind(ProfileKind v) => dbKind = v.index;
//
//   /// Relaciones: ToOne<X> / ToMany<X>. `.target` / `.add()` y después put().
//   // final owner = ToOne<UserEntity>();
// }

// ── Reglas de la casa ───────────────────────────────────────────────────────
//
// 1. **`openStore()` una sola vez**, en `main()`, antes del IoC. El store se
//    registra como singleton eager (ya está construido).
//
// 2. **Escrituras compuestas dentro de `store.runInTransaction(TxMode.write, …)`.**
//    Un wipe + rebuild a mitad de camino deja la data vieja intacta si falla;
//    sin transacción, queda un estado mixto que nadie sabe leer.
//
// 3. **El schema lo maneja el generador.** `dart run build_runner build` regenera
//    `objectbox.g.dart` + `objectbox-model.json`. Ese JSON tiene los UIDs de
//    cada campo: **se commitea y NUNCA se edita a mano**. Borrarlo hace que el
//    generador crea que todo es nuevo → data existente ilegible.
//
// 4. **Retirar una columna** = sacar el campo; el generador la anota en
//    `retiredPropertyUids`. ObjectBox no deja leer una columna ya retirada, así
//    que un backfill que la necesite requiere DOS releases (uno que copie, otro
//    que retire).
//
// 5. **La lógica no vive en la caja.** Las queries van en un repositorio de
//    `infra/`; lo determinístico (calcular, decidir, agregar) va en una función
//    pura de `domain/` que recibe la lista ya cargada. Así se testea sin abrir
//    un store.
//
// 6. **Tests:** el store es un plugin nativo. O se testea la capa pura sin él,
//    o se abre un store temporal por test
//    (`openStore(directory: Directory.systemTemp.createTempSync().path)`) y se
//    cierra + borra en el `tearDown`.
//
// 7. **Desktop.** `objectbox_flutter_libs` trae la librería nativa para las
//    plataformas Flutter soportadas por la versión que uses — verificar en el
//    changelog del paquete antes de prometer una plataforma. Una app Dart pura
//    (sin Flutter) necesita instalar la lib nativa aparte.
//
// pubspec:
//   dependencies:
//     objectbox: ^5.2.0
//     objectbox_flutter_libs: any
//     path: ^1.9.1
//     path_provider: ^2.1.5
//   dev_dependencies:
//     build_runner: ^2.4.14
//     objectbox_generator: any
