/// De dónde sale la portada de un juego.
///
/// # Por qué un NÚMERO y no una URL en el perfil
///
/// Porque una URL dentro de un perfil es un enlace que caduca guardado en un
/// fichero que nadie revisa, repetido en cada juego y copiado en cada perfil
/// que alguien comparta. El identificador de Steam, en cambio, no cambia nunca:
/// es el mismo número que ya usa la detección para saber si el juego está
/// instalado, así que el perfil no crece ni un campo y la dirección la arma
/// quien la va a pedir.
///
/// El campo era `cover_url` del lado de la ventana, y no llegaba de ningún
/// sitio: el cable no lo mandaba, así que estaba siempre vacío. Ver
/// `Game.steamAppId`.
///
/// # Las dos formas, medidas
///
/// Son dos porque los huecos del diseño son de dos formas, y estirar una en la
/// otra deja una banda del cartel o dos franjas vacías a los lados:
///
///   - **Vertical**, `library_600x900.jpg`, para los huecos altos: la
///     miniatura de la portada, la de la sala y la del diálogo.
///   - **Apaisada**, `header.jpg`, 460×215, para los anchos: la rejilla del
///     catálogo y la vista previa del alta manual.
///
/// Comprobadas el 2026-08-14 contra los nueve juegos del catálogo de fábrica
/// que tienen identificador de Steam: las dos contestan 200 con imagen. El
/// `header.jpg` de Don't Starve Together contesta 301 y la redirección lleva a
/// la imagen, que es lo que hace cualquier cliente HTTP por su cuenta.
///
/// # Qué NO se hace acá
///
/// Pedirle nada a la API de Steam. Esto son ficheros estáticos en su CDN, sin
/// clave, sin cuenta y sin decir quién pregunta más allá de la IP que pide una
/// imagen. Kanpachi no manda un identificador de nadie a ningún sitio para
/// dibujar una portada.
abstract final class SteamArt {
  static const String _cdn =
      'https://cdn.cloudflare.steamstatic.com/steam/apps';

  /// La portada vertical, 600×900. Null cuando el juego no está en Steam.
  static String? portrait(int? appId) =>
      appId == null || appId <= 0 ? null : '$_cdn/$appId/library_600x900.jpg';

  /// La cabecera apaisada, 460×215. Null cuando el juego no está en Steam.
  static String? landscape(int? appId) =>
      appId == null || appId <= 0 ? null : '$_cdn/$appId/header.jpg';
}
