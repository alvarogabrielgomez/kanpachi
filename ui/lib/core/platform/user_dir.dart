import 'dart:io';

/// Dónde escribe la VENTANA lo que es de quien la está mirando.
///
/// # La regla de los dos alcances
///
/// Kanpachi guarda en dos sitios, y el eje es máquina contra persona:
///
///  - `C:\ProgramData\Kanpachi` lo escribe el **daemon**, que corre como SYSTEM.
///    Ahí va lo de la MÁQUINA: el token, la identidad, la sala que hospeda, la
///    última a la que entró, el registro, la libreta de huellas y el nombre. Los
///    usuarios lo LEEN y no lo escriben, y esa ACL es media protección del
///    token: la pone el instalador a propósito.
///  - Esto lo escribe la **ventana**, como el usuario. Va lo de la PERSONA: su
///    registro de la interfaz y sus ajustes. El daemon no lo lee nunca.
///
/// Un ajuste de la ventana no es un hecho de la máquina. Dos personas en la
/// misma PC quieren cada una su tamaño de ventana y su modo verboso, y
/// guardarlos arriba los volvería compartidos, además de imposibles de escribir.
/// El nombre sí está arriba, y no es una excepción: es una decisión tomada, que
/// la identidad es de la máquina y tres caras preguntan por ella.
///
/// # Por qué esto existe como sitio propio y no como un respaldo
///
/// Porque ya se usaba sin que nadie lo hubiera decidido. `AppLog` cayó acá
/// porque no pudo escribir arriba, y lo dejó puesto como último recurso; el
/// resultado es que el sitio existía, funcionaba, y no estaba escrito en ningún
/// lado. El siguiente que guardara algo volvía a apuntar a la carpeta donde la
/// ventana no puede escribir, que es exactamente lo que pasó con los ajustes.
///
/// # La copia portable junta los dos, y eso no lo rompe
///
/// Una copia portable es una persona y una máquina a la vez: todo lo que
/// recuerda vive en la carpeta desde la que se abrió, y ahí sí puede escribir.
/// Por eso quien busca sitio prueba PRIMERO la carpeta del daemon: en portable
/// gana esa, y en el producto instalado no se puede y se cae acá.
abstract final class UserDir {
  static const String folder = 'Kanpachi';

  /// La carpeta, o null si el sistema no dice dónde.
  ///
  /// No la crea: eso lo hace quien vaya a escribir, que además es el único que
  /// puede comprobar que de verdad se puede. Crear la carpeta sale bien en
  /// sitios donde crear un archivo dentro no, que es justo la forma del permiso
  /// de `C:\ProgramData`.
  static String? get path {
    final String? base = Platform.environment['LOCALAPPDATA'];
    if (base == null || base.isEmpty) return null;
    return '$base${Platform.pathSeparator}$folder';
  }
}
