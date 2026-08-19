/// Qué motor lleva esta instalación, tal como lo contesta el daemon.
///
/// El daemon lo lee de los centinelas que el motor lleva sellados en su
/// fichero, así que esto describe lo INSTALADO: el proceso vivo se delata solo
/// en su log. Vacío significa un motor anterior al centinela, que es un
/// hallazgo normal y no un fallo.
class EngineInfo {
  const EngineInfo({this.build = '', this.lib = ''});

  /// `0.1.0+g9486f08cd21a`, o vacío.
  final String build;

  /// `easytier@v2.6.4-kanpachi.1`, o vacío.
  final String lib;

  bool get known => build.isNotEmpty || lib.isNotEmpty;

  /// La forma humana: `0.1.0+g9486f08cd21a (easytier@v2.6.4-kanpachi.1)`.
  @override
  String toString() {
    if (build.isEmpty && lib.isEmpty) return '';
    if (lib.isEmpty) return build;
    if (build.isEmpty) return '($lib)';
    return '$build ($lib)';
  }
}
