//go:build !windows

package kanpachi

// Preflight no comprueba nada fuera de Windows, y eso NO es un pendiente.
//
// Lo que en Windows hay que sondear es un driver de terceros que se instala en
// el almacén de drivers del sistema, con su `.inf` y su firma, y que puede
// negarse por motivos que no se ven mirando archivos. Ver [Preflight] en
// `preflight_windows.go`.
//
// En Linux no hay nada de eso: el nodo de TUN lo pone el kernel, no hay almacén,
// no hay `.inf` y no hay instalación que pueda fallar a medias. Lo único que
// puede faltar es `/dev/net/tun`, y eso ya se mira donde corresponde, que es
// `kanpachi doctor` —`chequeoDeTUN` en `doctor_linux.go`—, con sus números de
// dispositivo y su arreglo de una línea.
//
// Devolver `nil` acá es la respuesta correcta y no una omisión: inventar una
// comprobación que siempre contesta que sí llenaría el arranque de ruido y haría
// creer que se midió algo.
func Preflight(string) error { return nil }
