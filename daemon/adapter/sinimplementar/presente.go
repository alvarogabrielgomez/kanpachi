package sinimplementar

// Este archivo tenía una constante, `Presente`, y ya no.
//
// # Qué decía, y por qué se fue
//
// Decía si este binario llevaba adaptadores provisionales dentro, con toda una
// explicación de por qué la etiqueta de compilación iba al revés de lo
// intuitivo. La explicación era buena y el mecanismo no: era un `const` a mano,
// y la etiqueta `release` que lo gobernaba **no existía en ningún archivo del
// repositorio**, así que `go build -tags release` producía un binario idéntico
// al de siempre. La protección era una intención escrita, no una comprobación.
//
// Ahora la pregunta se la hace `cmd/kanpachid` al cableado, con [Names] sobre
// los adaptadores que de verdad eligió. La diferencia que importa no es de
// estilo: una constante hay que acordarse de encenderla el día que un
// provisional VUELVA, y ese olvido es silencioso y caro. Una comprobación sobre
// el cableado no se puede olvidar, porque cablear el provisional ES lo que la
// dispara.
//
// El riesgo del que hablaba sigue siendo el mismo y sigue vigente: nunca fue
// que los provisionales fallen, fue que un binario con un firewall que dice que
// purgó termine instalado en la máquina de alguien.
