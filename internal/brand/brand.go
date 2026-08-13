// Package brand es lo que identifica a QUIEN publicó este binario, en un solo
// sitio.
//
// # Qué va acá
//
// El repositorio del que salen las publicaciones, el interruptor que apaga la
// comprobación de versiones, y de ahí cuelgan las URL que este proyecto consulta
// o reparte. **Un fork toca este fichero y ninguno más.**
//
// # Qué NO va acá, y hay que decirlo porque es la trampa
//
// Nada que las dos máquinas de una sala tengan que calcular igual. Los
// parámetros de Argon2id (`core/domain/identity.go`), el alfabeto del invite ID
// y la versión fijada de EasyTier (`registry/setup`) parecen configuración y no
// lo son: están congelados, con vector dorado en los tests, porque los dos lados
// derivan por separado y sin hablarse. Un fork que los "configure" produce salas
// donde la gente pega el mismo código y se queda sola, sin ningún síntoma que
// apunte a la causa.
//
// La regla: acá va lo de quien PUBLICA, jamás lo del protocolo.
//
// # Por qué en internal/ y no en core/domain
//
// Por lo mismo que [selfupdate]: `internal/` a secas lo importa todo el módulo y
// no lo importa nadie de fuera, así que sirve al daemon, al registro, al
// instalador y a las sondas a la vez. En `core/domain` estaría mezclado con las
// reglas de la sala, que es exactamente la confusión que la sección de arriba
// existe para impedir.
//
// # Las otras caras
//
// Dart no puede importar esto, así que lo espeja en `ui/lib/core/brand.dart`,
// atado con un test de lockstep. El JavaScript de la página **no lo espeja**: lo
// recibe del servidor en el hueco de estado, ver `registry.estadoDeLaPagina`. Y
// el motor de Rust no lleva ninguno, porque no tiene ninguna constante de marca
// en su código: lo único suyo es el campo `repository` de su `Cargo.toml`, que
// es el sitio canónico de Rust para eso y encima nombra a otro repositorio.
package brand

// Repo es EL CANAL DE ACTUALIZACIÓN, y es la constante que hay que tocar para
// moverlo.
//
// De acá salen las tres URL que este proyecto consulta: la que dice qué versión
// hay, la base de la que cuelgan los artefactos de un tag, y la página a la que
// se manda a una persona. **Cambiar esta línea mueve las tres**, y un guardián
// de `internal/arch` falla si el nombre aparece escrito en cualquier otro sitio,
// para que nadie deje media actualización apuntando al canal de otro.
//
// **Es distinto del registro de una sala**, y confundirlos es fácil porque hasta
// el 2026-08-12 compartían la cadena `kanpachi.accentio.dev`. El registro es de
// cada sala y viaja dentro del código; esto es de quien COMPILÓ este binario, y
// contesta de dónde salió y a dónde volver a buscar. Un fork cambia esto y
// ninguna de sus salas se entera.
//
// OJO: no coincide con la ruta del módulo (`accentiostudios/kanpachi`). El
// módulo se nombró así antes de que el repositorio existiera, y quien los
// confunda escribe una URL que devuelve 404.
const Repo = "alvarogabrielgomez/kanpachi"

// UpdatesEnabled es si esta compilación comprueba versiones.
//
// # Para quien forkea y publica por otro canal, o no publica
//
// En `false` **no se consulta nada, nunca**, y las caras se callan en vez de
// enseñar un fallo. Sin este interruptor la única salida sería dejar [Repo]
// apuntando a un repositorio que no existe, y eso no apaga la comprobación: la
// convierte en un 404 cada vez que alguien la pide, o sea en una pantalla que
// dice que algo va mal cuando lo que pasa es que ese fork no publica ahí.
//
// Es una constante y no un ajuste porque es una decisión de quien COMPILA, igual
// que [Repo]. Quien usa el programa no elige de dónde salió su binario.
const UpdatesEnabled = true

// Docs es a dónde apunta el `Documentation=` de las units y el sitio del
// instalador. El repositorio, y no un dominio: un dominio es de alguien que
// paga un DNS, y el repositorio es de donde salió esto.
const Docs = "https://github.com/" + Repo
