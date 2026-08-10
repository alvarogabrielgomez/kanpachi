package nftpermits

// La auditoría de lo que hay en el sistema y no puso Kanpachi.
//
// # Qué se busca, y por qué son dos cosas distintas
//
// En Windows lo ajeno peligroso es un PERMISO: una regla que abre un puerto sin
// pasar por Kanpachi, y la más cara de todas es la de escritorio remoto, porque
// entrega la máquina entera por la red virtual.
//
// En Linux hay que buscar las dos direcciones, y la segunda no existe allá:
//
//   - Lo que ABRE de más. Igual que en Windows.
//   - Lo que CIERRA. Un ufw o un firewalld con la entrada denegada bloquea el
//     puerto del juego, y Kanpachi **no puede** abrir por encima. Sin reportarlo,
//     el operador ve una sala perfectamente montada donde no entra nadie y no
//     tiene dónde mirar.
//
// Se reporta con el comando exacto para resolverlo, y no se ejecuta ninguno.

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/accentiostudios/kanpachi/core/domain"
)

// Los gestores de firewall que se reconocen por nombre.
//
// La lista es CERRADA y corta a propósito. Reconocer uno de más produce un aviso
// que el operador no puede accionar; el resto se ve igual, como reglas de una
// tabla que no es nuestra, y eso ya se reporta.
var gestores = []struct {
	nombre  string
	unidad  string
	arreglo string
}{
	{"ufw", "ufw.service", "sudo ufw allow in on %s"},
	{"firewalld", "firewalld.service", "sudo firewall-cmd --zone=trusted --add-interface=%s --permanent && sudo firewall-cmd --reload"},
}

// AuditForeign mira qué hay puesto que Kanpachi no puso.
//
// # Nunca falla el arranque de una sala
//
// Un fallo leyendo el ruleset devuelve error, y quien llama lo convierte en la
// alerta de "no se pudo auditar". Lo que NO hace es inventarse una lista vacía,
// que se leería como "no hay nada raro" y es la mentira que esta función existe
// para no decir.
func (p *Permits) AuditForeign(ctx context.Context, prof domain.GameProfile) ([]domain.ForeignRule, error) {
	p.mu.Lock()
	adapter := p.adapter
	p.mu.Unlock()

	if adapter == "" {
		// Sin adaptador no hay red virtual, así que no hay nada que alguien
		// pueda estar alcanzando por ella. Es el caso normal del daemon en
		// reposo, no un fallo.
		return nil, nil
	}

	salida, err := listRuleset(ctx)
	if err != nil {
		return nil, err
	}

	var out []domain.ForeignRule
	out = append(out, p.quarantineGaps()...)
	out = append(out, blockingManagers(ctx, adapter)...)
	out = append(out, foreignOpeners(salida, adapter, prof)...)
	return out, nil
}

// quarantineGaps reporta los puertos que la cuarentena dejó abiertos.
//
// Salen por acá y no solo por el log porque son un recorte de una promesa del
// producto, y un recorte que solo se anota en el log es un recorte silencioso.
// La pantalla diría que la cuarentena está puesta, y lo está a medias.
func (p *Permits) quarantineGaps() []domain.ForeignRule {
	p.mu.Lock()
	saltados := append([]uint16(nil), p.skipped...)
	p.mu.Unlock()

	out := make([]domain.ForeignRule, 0, len(saltados))
	for _, puerto := range saltados {
		out = append(out, domain.ForeignRule{
			Name: fmt.Sprintf("el puerto %d lo cerraría la cuarentena de Kanpachi y hay "+
				"algo escuchando ahí, así que se dejó abierto. Cerrarlo podía dejar fuera a "+
				"quien administra esta máquina. Para ver qué es: sudo ss -lntp sport = :%d",
				puerto, puerto),
			Executable: fmt.Sprintf("puerto %d", puerto),
			WasEnabled: true,
		})
	}
	return out
}

// blockingManagers reporta los gestores de firewall que están activos.
//
// # Por qué se mira la unidad de systemd y no las reglas
//
// Porque lo que importa no es qué regla concreta tiene puesta hoy, sino que hay
// alguien más gobernando el firewall de esta máquina. Un ufw activo puede estar
// permitiendo todo ahora y denegando dentro de un minuto, y el operador tiene
// que enterarse igual.
//
// Se reporta SIEMPRE que esté activo, incluso permitiendo. La alternativa era
// interpretar su política, y hacerlo mal en la dirección optimista es decir que
// no hay problema justo cuando lo hay.
func blockingManagers(ctx context.Context, adapter string) []domain.ForeignRule {
	var out []domain.ForeignRule
	for _, g := range gestores {
		if !unitActive(ctx, g.unidad) {
			continue
		}
		out = append(out, domain.ForeignRule{
			Name: fmt.Sprintf("%s está gobernando el firewall de esta máquina. "+
				"Kanpachi no puede abrir por encima de un bloqueo suyo: en netfilter "+
				"un drop corta la evaluación y un accept no es final. Si no entra "+
				"nadie a la sala, es el primer sitio donde mirar. Para dejar pasar el "+
				"adaptador virtual: "+g.arreglo, g.nombre, adapter),
			Executable: g.unidad,
			// El nombre del gestor va en el nombre porque la pantalla muestra eso.
			// La clase la calcula el dominio, jamás el adaptador.
			WasEnabled: true,
		})
	}
	return out
}

// unitActive pregunta a systemd si una unidad está corriendo.
//
// Se invoca `systemctl` con argumentos literales y sin shell. No hay
// interpolación de nada que venga de fuera: los nombres de unidad salen de la
// lista cerrada de arriba.
func unitActive(ctx context.Context, unit string) bool {
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", unit)
	return cmd.Run() == nil
}

// foreignOpeners busca reglas de OTRAS tablas que abran lo que no deberían.
//
// # Qué cuenta como ajeno
//
// Cualquier `accept` que nombre el adaptador virtual o un puerto del perfil, y
// que esté fuera de nuestras dos tablas. Las cadenas de Docker entran por acá,
// y son el caso frecuente en un servidor de juegos: Docker inserta las suyas y
// se saltan lo que el operador crea que tiene configurado.
//
// # Por qué se lee el texto de `nft list ruleset` y no netlink
//
// Porque acá se AUDITA, y lo que hay que enseñarle al operador es exactamente la
// línea que él va a ver cuando corra el mismo comando. Reconstruirla desde
// netlink produciría una frase parecida y no idéntica, y una regla que el
// operador no encuentra tal cual se la mostraron es una regla que no va a tocar.
//
// Para ESCRIBIR sigue siendo netlink, en la compuerta. Leer y escribir tienen
// requisitos distintos y por eso usan vías distintas.
func foreignOpeners(ruleset, adapter string, prof domain.GameProfile) []domain.ForeignRule {
	puertos := portTokens(prof)

	var out []domain.ForeignRule
	var tabla string
	sc := bufio.NewScanner(strings.NewReader(ruleset))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		linea := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(linea, "table ") {
			tabla = linea
			continue
		}
		if esNuestra(tabla) || !strings.Contains(linea, "accept") {
			continue
		}
		if !strings.Contains(linea, adapter) && !mencionaPuerto(linea, puertos) {
			continue
		}
		out = append(out, domain.ForeignRule{
			Name:       linea,
			Executable: strings.TrimSuffix(strings.TrimPrefix(tabla, "table "), " {"),
			WasEnabled: true,
		})
	}
	return out
}

// esNuestra dice si una cabecera `table ...` es de Kanpachi.
//
// Por el nombre exacto y jamás por prefijo: una tabla ajena llamada
// `kanpachi-loquesea` no es nuestra, y tratarla como propia sería dejar de
// auditar justo lo que alguien puso para pasar desapercibido.
func esNuestra(tabla string) bool {
	campos := strings.Fields(strings.TrimSuffix(tabla, " {"))
	if len(campos) < 3 {
		return false
	}
	nombre := campos[2]
	return nombre == "kanpachi" || nombre == QuarantineTable
}

// portTokens son los puertos del perfil, como cadenas que buscar en el texto.
func portTokens(prof domain.GameProfile) []string {
	var out []string
	for _, rangos := range [][]domain.PortRange{prof.HostPorts, prof.ClientPorts} {
		for _, r := range rangos {
			if r.From == r.To {
				out = append(out, fmt.Sprintf("%d", r.From))
				continue
			}
			out = append(out, fmt.Sprintf("%d-%d", r.From, r.To))
		}
	}
	return out
}

func mencionaPuerto(linea string, puertos []string) bool {
	for _, p := range puertos {
		// Con delimitador, para que 1626 no case dentro de 16261.
		if strings.Contains(linea, " "+p+" ") || strings.HasSuffix(linea, " "+p) {
			return true
		}
	}
	return false
}

// listRuleset devuelve el ruleset entero como texto.
//
// Se invoca el binario `nft` y no netlink por lo dicho en [foreignOpeners]. Sin
// shell y sin argumentos que vengan de fuera.
func listRuleset(ctx context.Context) (string, error) {
	if _, err := exec.LookPath("nft"); err != nil {
		return "", fmt.Errorf("no está `nft`, así que no se puede auditar el firewall "+
			"de esta máquina: %w", err)
	}
	salida, err := exec.CommandContext(ctx, "nft", "list", "ruleset").Output()
	if err != nil {
		return "", fmt.Errorf("leyendo el ruleset: %w", err)
	}
	return string(salida), nil
}

// loadQuarantine carga el fichero recién escrito.
//
// Se le pasa por la ENTRADA ESTÁNDAR y no por ruta. La diferencia importa: `nft
// -f` sobre una ruta la vuelve a leer del disco, y entre escribir y cargar cabe
// que alguien la haya cambiado. Cargando lo mismo que se escribió, lo que queda
// puesto es lo que este proceso decidió.
func loadQuarantine(ctx context.Context, texto string) error {
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("no está `nft`, así que la cuarentena de base no se puede "+
			"cargar. Se escribió el fichero y se cargará al arrancar: %w", err)
	}
	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(texto)
	salida, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cargando la cuarentena: %w: %s", err, strings.TrimSpace(string(salida)))
	}
	return nil
}

// QuarantineLoaded dice si la tabla de la cuarentena está puesta AHORA.
//
// La usa `kanpachi doctor`, y es la comprobación que más importa de todas: si
// esa tabla no está, los puertos del juego están abiertos al mundo y nada más en
// el sistema lo dice.
func QuarantineLoaded(ctx context.Context) (bool, error) {
	salida, err := listRuleset(ctx)
	if err != nil {
		return false, err
	}
	return strings.Contains(salida, "table inet "+QuarantineTable+" "), nil
}

// FileOnDisk dice si el fichero de la cuarentena está escrito.
//
// Va aparte de [QuarantineLoaded] porque son dos fallos distintos con dos
// arreglos distintos: cargada y sin fichero se pierde en el próximo reinicio;
// con fichero y sin cargar, la unidad no corrió.
func (p *Permits) FileOnDisk() (bool, error) {
	_, err := os.Stat(p.file)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
