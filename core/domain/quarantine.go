package domain

import "fmt"

// La cuarentena de base: los bloqueos que sobreviven al servicio detenido.
//
// # Qué protege, en una frase
//
// Que un juego, un instalador o un diálogo de Windows deje una regla permisiva
// sobre SMB o Escritorio remoto y la máquina quede alcanzable por ahí. Los
// bloqueos explícitos del Firewall de Windows ganan sobre cualquier permiso en
// conflicto, sin desempate por especificidad, así que un bloqueo puesto acá le
// gana a esa regla ajena esté quien esté escuchando.
//
// # Por qué no es un deny-all
//
// Porque no hace falta y porque no podría. La entrada ya viene bloqueada por
// defecto en los tres perfiles de Windows, o sea que **la ausencia de reglas de
// permiso YA ES el deny-all**, y además es uno que ninguna regla de Kanpachi
// puede tapar. Un bloqueo total escrito acá taparía las reglas del juego activo
// que crea el propio daemon, por la misma mecánica de arriba. Ver decisión 4.
//
// El bloqueo de todo SÍ existe, y vive en la OTRA capa: la compuerta de WFP
// sobre el adaptador virtual, donde un bloqueo sí admite excepciones por encima.
// Confundir las dos capas lleva a "corregir" cualquiera de las dos en la
// dirección equivocada.
//
// # Por qué lo escribe el daemon
//
// Antes lo iba a poner un instalador. No hay instalador, y una cuarentena que
// depende de un programa que no existe es una promesa apagada. El daemon la
// escribe y la REPONE en cada arranque, con un método que solo sabe agregar.
//
// Lo que cambia es quién la pone, y lo que NO cambia es lo que la hace valiosa:
// nadie la borra al apagarse, así que sigue puesta con el servicio detenido,
// deshabilitado o a medio desinstalar. Por eso [FirewallGroupBase] sigue siendo
// un grupo aparte de [FirewallGroup]: la purga del arranque se lleva el de la
// sala y no toca este.

// QuarantineRule es una regla de la cuarentena de base.
//
// # No hay campo de acción, y la ausencia ES la garantía
//
// Cada regla de este tipo es un BLOQUEO por construcción del tipo. No existe
// forma de escribir un permiso con él, ni por descuido ni a propósito.
//
// Por eso es un tipo aparte y no un campo `Action` en [FirewallRule]: aquel es
// el que produce [BuildRuleSet] a partir de un perfil del catálogo, o sea de un
// archivo que el usuario puede importar. Con un campo de acción ahí, **un perfil
// de juego podría emitir bloqueos**. Hoy la invariante "un perfil solo ABRE" la
// sostiene el tipo; con ese campo la sostendría la disciplina de quien escriba
// el código.
//
// # Qué NO lleva, y cada ausencia tiene su motivo
//
//   - **Alcance por adaptador.** Un alcance que deja de casar convierte un
//     permiso en un cierre y **un bloqueo en nada**. La cuarentena vale para
//     toda la máquina o no vale. Lo mismo dice [FirewallRule.Local] al revés.
//   - **Alcance remoto.** Estos bloqueos son contra cualquiera, que es lo único
//     que tiene sentido para "mi PC no ofrece SMB".
//   - **Perfil de red.** Van en los tres, porque el producto no depende de que
//     Windows clasifique bien la red.
type QuarantineRule struct {
	// Name es determinista a partir del resto de campos, igual que en
	// [FirewallRule]: de eso depende que reponer la cuarentena pueda comparar
	// contra lo vivo sin guardar identificadores en ningún sitio.
	Name  string
	Proto Proto
	From  uint16
	To    uint16
	// In es la dirección. Cada puerto se bloquea en las DOS, y son dos reglas
	// porque una regla de Windows tiene una dirección y solo una.
	//
	// **El puerto es siempre el LOCAL, en las dos direcciones**, y de eso
	// depende que la cuarentena no rompa la máquina. Entrante con puerto local
	// 445 es "nadie llega a MI servicio de SMB", que es la protección. Saliente
	// con puerto local 445 es "mi servicio de SMB tampoco habla hacia afuera",
	// que cierra el mismo servicio por el otro lado.
	//
	// Lo que NO hace, y sería un daño de verdad si lo hiciera: **no impide que
	// esta máquina sea CLIENTE**. Montar un disco de red, entrar por Escritorio
	// remoto a otra PC o usar git por SSH salen de un puerto local efímero hacia
	// el 445, el 3389 o el 22 del OTRO, así que ninguna de estas reglas los
	// toca. Bloquear por puerto remoto sí los rompería, y para siempre, porque
	// la cuarentena sigue puesta con Kanpachi apagado.
	In bool
}

// BaseQuarantine devuelve la cuarentena entera.
//
// **No recibe parámetros a propósito.** No hay nada que un llamador pueda
// ajustar: ni qué puertos, que salen de [forbiddenPorts] y son la misma lista
// que ningún perfil puede pedir, ni el alcance, que no existe. Un parámetro acá
// sería una forma de escribir una cuarentena más floja.
//
// Sale ordenada y es la misma lista en cada llamada, así que reponerla es
// comparable contra lo vivo.
//
// # Sin permiso de ICMP echo
//
// Una versión anterior de esto prometía un permiso de ICMP echo "para el
// diagnóstico". No se escribe, y el motivo es que **ninguna función del producto
// depende de él**: el sondeo de MTU manda el ping hacia AFUERA, que la salida no
// bloquea, y la latencia de un miembro la mide el motor por su propio protocolo.
// Sería la única regla de la cuarentena que ABRE en vez de cerrar, y sin acotar,
// o sea contestando el ping en toda red a la que la máquina se conecte, para
// siempre y con Kanpachi apagado. Se paga una superficie permanente por un
// diagnóstico que nadie usa.
func BaseQuarantine() []QuarantineRule {
	// Los dos protocolos para cada puerto, sin mirar cuál usa cada servicio de
	// verdad. Un puerto bloqueado que nadie usa no cuesta nada; uno abierto
	// porque alguien recordó mal qué protocolo era cuesta justo lo que esta
	// lista existe para impedir.
	protos := [...]Proto{ProtoTCP, ProtoUDP}
	dirs := [...]bool{true, false}

	out := make([]QuarantineRule, 0, len(forbiddenPorts)*len(protos)*len(dirs))
	for _, port := range forbiddenPorts {
		for _, proto := range protos {
			for _, in := range dirs {
				out = append(out, QuarantineRule{
					Name:  quarantineName(proto, port, in),
					Proto: proto,
					From:  port,
					To:    port,
					In:    in,
				})
			}
		}
	}
	return out
}

// quarantineName arma el nombre que va a ver el usuario en la consola del
// Firewall de Windows.
//
// En castellano y con el puerto dentro porque ahí es donde se lee: alguien
// mirando por qué su SMB no contesta tiene que poder encontrar la causa sin
// abrir el código. Es la misma razón por la que la lista de arriba no se
// agrupa en rangos, que dejaría "137-139" donde el usuario busca "139".
func quarantineName(p Proto, port uint16, in bool) string {
	sentido := "saliente"
	if in {
		sentido = "entrante"
	}
	proto := "TCP"
	if p == ProtoUDP {
		proto = "UDP"
	}
	return fmt.Sprintf("%s: bloqueo %s %s %d", FirewallGroupBase, sentido, proto, port)
}
