package sinimplementar

// Presente dice si este binario lleva adaptadores provisionales dentro.
//
// # Por qué la etiqueta va AL REVÉS de lo que parece
//
// El build con provisionales es el POR DEFECTO, y el de release lleva la
// etiqueta explícita `release`. Al revés sería lo intuitivo y rompería el CI:
// el único paso del job de Windows es `go build ./...`, sin etiquetas, así que
// un build por defecto que no compilara lo dejaría en rojo permanente.
//
// Y hay una razón mejor que el CI. Con la etiqueta en el lado de los
// provisionales, olvidarla produce un binario de release SILENCIOSO: compila,
// arranca, se instala como servicio y no hace nada de lo que promete. Con la
// etiqueta en el lado de release, olvidarla produce un binario que se NIEGA a
// instalarse. El olvido tiene que doler del lado seguro.
//
// # Qué hace con esto quien lo lee
//
// `cmd/kanpachid` se niega a correr como servicio de Windows cuando esto es
// true, y solo arranca con `--console`. El riesgo de verdad nunca fue que los
// provisionales fallen: es que un binario con un firewall que dice que purgó
// termine instalado en la máquina de alguien.
const Presente = true
