// Package nftpermits es la capa que ABRE, en Linux, y casi todo lo que hace es
// no abrir nada.
//
// # Por qué existe si no abre
//
// El contrato de [firewall.Permits] tiene siete métodos y en Windows los siete
// escriben o leen el Firewall de Windows. En Linux el reparto es otro, y la
// razón es del sistema, no de esta implementación:
//
//   - ABRIR no hace falta. Un Ubuntu recién instalado no bloquea nada entrante,
//     así que no hay nada que abrir para que el juego reciba. Y donde el
//     operador SÍ tiene un ufw o un firewalld cerrando, Kanpachi **no puede**
//     abrir por encima: en netfilter un `drop` corta la evaluación de todas las
//     cadenas del hook, y un `accept` no es final. Es la misma propiedad que
//     hace fuerte a nuestra compuerta, y no se puede tener una sin la otra.
//   - CERRAR sí, y es lo importante: la cuarentena de base tiene que sobrevivir
//     a Kanpachi apagado, y la compuerta no puede sostenerla porque se va con la
//     tabla al salir.
//   - AUDITAR también, porque lo ajeno que bloquea o que abre de más es lo único
//     que queda por decir cuando no se puede tocar.
//
// # La regla que gobierna todo este paquete
//
// **Solo se toca lo nuestro.** Nuestras tablas y nuestro fichero. Lo del
// operador (ufw, firewalld, las cadenas de Docker, el kernel) se lee, se reporta
// con el comando exacto para arreglarlo, y no se ejecuta nada. Es la misma
// decisión que llevó a bifurcar EasyTier: las dos llamadas que se le quitaron
// escribían reglas permanentes en el firewall del usuario que sobrevivían al
// proceso.
package nftpermits

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/daemon/adapter/safewrite"
)

// QuarantineTable es la tabla donde vive la cuarentena de base.
//
// Aparte de la de la compuerta a propósito: la compuerta se borra entera al
// salir de la sala y la cuarentena tiene que quedarse. Con las dos en la misma
// tabla, barrer una se llevaría la otra, y eso es exactamente el fallo que la
// cuarentena existe para que no ocurra.
const QuarantineTable = "kanpachi-base"

// QuarantineFile es dónde se escribe para que sobreviva al reinicio.
//
// # Por qué un fichero propio y no `/etc/nftables.conf`
//
// Porque ese fichero es del operador. Meterle nuestras reglas significa que un
// `apt purge` suyo se lleva las nuestras, que su edición pisa las nuestras, y
// que quien lea su firewall encuentra cosas que no puso. El fichero propio lo
// carga una unidad propia, y desinstalar Kanpachi se lleva las dos.
const QuarantineFile = "/etc/kanpachi/quarantine.nft"

// ErrNoSuspend es lo que se devuelve al pedir suspender una regla ajena.
//
// nftables no tiene "deshabilitar una regla": o está o no está. Borrarla y
// recordarla para reponerla después sería escribir en la configuración del
// operador, que es justo lo que este paquete no hace. Se dice y se explica cómo
// hacerlo a mano.
var ErrNoSuspend = errors.New("en Linux una regla ajena no se puede suspender: " +
	"nftables no tiene deshabilitar-una-regla, y borrarla sería escribir en la " +
	"configuración del operador. Se muestra cuál es y se deja la decisión afuera")

// Logger es la rebanada del log del daemon que hace falta acá.
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// Permits es la capa que abre, o sea la que audita.
type Permits struct {
	mu      sync.Mutex
	adapter string
	file    string
	log     Logger
	// skipped son los puertos de la cuarentena que se dejaron abiertos porque
	// había algo escuchando en ellos.
	//
	// Se guardan para que salgan por [Permits.AuditForeign], y no solo en el
	// log. Un recorte que solo se anota en el log es un recorte silencioso: la
	// pantalla diría que la cuarentena está puesta, y lo estaría a medias.
	skipped []uint16
}

// splitInUse parte la cuarentena entre lo que hay que saltar y lo que se cierra.
//
// Devuelve los PUERTOS saltados y las REGLAS que quedan, que son dos formas
// distintas porque cada puerto produce cuatro reglas y lo que hay que reportar
// es el puerto.
func splitInUse(rules []domain.QuarantineRule, escuchando map[uint16]bool) ([]uint16, []domain.QuarantineRule) {
	var puertos []uint16
	vistos := map[uint16]bool{}
	quedan := make([]domain.QuarantineRule, 0, len(rules))

	for _, r := range rules {
		if !escuchando[r.From] {
			quedan = append(quedan, r)
			continue
		}
		if !vistos[r.From] {
			vistos[r.From] = true
			puertos = append(puertos, r.From)
		}
	}
	sort.Slice(puertos, func(i, j int) bool { return puertos[i] < puertos[j] })
	return puertos, quedan
}

// New abre la capa.
//
// `file` vacío usa [QuarantineFile]. Se puede cambiar para las pruebas, y no
// para el producto: el `.deb` instala la unidad que carga esa ruta y las dos
// tienen que decir lo mismo.
func New(file string, log Logger) (*Permits, error) {
	if log == nil {
		return nil, errors.New("los permisos necesitan un log")
	}
	if file == "" {
		file = QuarantineFile
	}
	return &Permits{file: file, log: log}, nil
}

func (p *Permits) Close() error { return nil }

// SetAdapter anota el nombre del adaptador virtual cuando el motor lo crea.
func (p *Permits) SetAdapter(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.adapter = name
}

// Apply no escribe nada, y devolver nil acá es la respuesta correcta.
//
// Quien abre de verdad en Linux es la compuerta, que ya corrió antes que esto:
// [firewall.Firewall.Apply] la aplica primero y solo llega acá si funcionó.
// Escribir una segunda copia de las mismas reglas en otra tabla no añadiría
// permiso, porque en netfilter dos `accept` no abren más que uno, y sí añadiría
// un sitio más donde quedar desincronizado.
//
// **No se traga un fallo**: si hubiera algo que abrir y no se pudiera, quien lo
// diría es la compuerta, fallando antes de llegar acá.
func (p *Permits) Apply(context.Context, domain.RuleSet) error { return nil }

// PurgeOwned no tiene nada que purgar por el mismo motivo que [Permits.Apply] no
// tiene nada que escribir.
//
// Y jamás toca [QuarantineTable]. Purgar es "quitar lo de la sala"; la
// cuarentena no es de la sala, es lo que queda puesto cuando no hay ninguna.
func (p *Permits) PurgeOwned(context.Context) error { return nil }

// SuspendForeign niega, y explica.
func (p *Permits) SuspendForeign(context.Context, []domain.ForeignRule) error {
	return ErrNoSuspend
}

// RestoreForeign no falla, y es la única asimetría con [Permits.SuspendForeign].
//
// Su contrato es "dejar la máquina sin nada pendiente", y donde nunca se
// suspendió nada eso ya es cierto. Devolver error acá haría fallar el apagado de
// un daemon que no tiene nada que deshacer. Es el mismo criterio que
// `netcfg_other.go` aplica a `RevertTweaks`.
func (p *Permits) RestoreForeign(context.Context) error { return nil }

// FirewallEnabled devuelve una lista VACÍA, y eso es un hecho, no un hueco.
//
// Los perfiles de firewall son de Windows: dominio, privado y público. En Linux
// no existen, así que no hay ninguno que pueda estar apagado.
//
// Las dos alternativas eran peores y las dos mienten. Devolver los tres
// encendidos pinta de verde algo que no se midió. Devolver error levanta la
// alerta de "no se pudo comprobar" para siempre, que entrena a ignorarla.
// Devolver vacío hace que [usecase.diagnose] no emita alerta, que es lo correcto:
// lo que sí protege acá se mide aparte, y son la compuerta y la cuarentena.
func (p *Permits) FirewallEnabled(context.Context) ([]domain.FirewallProfileState, error) {
	return nil, nil
}

// Enforcement devuelve VACÍO y la compuerta en ausente, tal como pide el
// contrato de [firewall.PermitAudit].
//
// Las reglas vivas las reporta la compuerta, que en Linux es la misma tabla, y
// las emite en las dos capas por eso mismo. Ver `nft.Gate.Measure`. Devolverlas
// también acá sería contarlas dos veces desde dos lectores que pueden discrepar.
func (p *Permits) Enforcement(context.Context) (domain.Enforcement, error) {
	return domain.Enforcement{Gate: domain.GateAbsent}, nil
}

// ApplyBaseQuarantine escribe la cuarentena y la deja cargada.
//
// # Solo AGREGA, jamás borra
//
// Es el contrato del método en las dos plataformas, y acá se cumple de una forma
// concreta: se genera el fichero entero desde la lista que llega, y esa lista es
// [domain.BaseQuarantine], que no recibe parámetros y no depende de ninguna
// sala. O sea que "regenerar" y "agregar lo que falte" son la misma operación,
// y no hay ningún camino por el que una sala pueda quitar un puerto de acá.
//
// # El fichero primero y la carga después
//
// Al revés, un fallo al escribir dejaría la cuarentena puesta en memoria y
// ausente en el próximo arranque, que es la mitad peor: protege hoy y no mañana,
// sin que nada lo diga.
func (p *Permits) ApplyBaseQuarantine(ctx context.Context, rules []domain.QuarantineRule) error {
	if len(rules) == 0 {
		// Vacío significaría dejar la máquina sin cuarentena, y eso no se pide
		// nunca: [domain.BaseQuarantine] no recibe parámetros.
		return errors.New("la cuarentena de base llegó vacía, y vaciarla no es una operación")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Qué hay escuchando, ANTES de decidir qué cerrar. Ver `listening_linux.go`:
	// el dominio ya saca el 22, y esto cubre al operador que movió sshd a otro
	// puerto de la lista, que el dominio no puede saber.
	//
	// Un fallo leyendo esto NO se traga: sin saber qué hay escuchando, cerrar es
	// apostar a que nada importante vive ahí.
	escuchando, err := listeningPorts()
	if err != nil {
		return fmt.Errorf("antes de cerrar puertos hay que saber cuáles están en uso: %w", err)
	}

	usados, quedan := splitInUse(rules, escuchando)
	p.skipped = usados
	for _, r := range usados {
		// Se anota UNA por puerto y no una por regla: son cuatro reglas por
		// puerto y cuatro líneas iguales invitan a leer una y descartar el resto.
		p.log.Warn("puerto de la cuarentena con algo escuchando: se deja abierto",
			"puerto", r, "porqué", "cerrarlo dejaría fuera a quien administra esta máquina")
	}
	if len(quedan) == 0 {
		return fmt.Errorf("los %d puertos de la cuarentena tienen algo escuchando, así que "+
			"no queda ninguno que cerrar sin romper algo. Revisar qué corre en esta máquina",
			len(usados))
	}

	texto, err := quarantineScript(quedan)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p.file), 0o755); err != nil {
		return fmt.Errorf("creando la carpeta de la cuarentena: %w", err)
	}
	// Escritura atómica, por lo mismo que el resto del estado: un fichero a
	// medias que la unidad cargue al arrancar es una cuarentena a medias.
	if err := safewrite.File(p.file, []byte(texto), 0o644); err != nil {
		return fmt.Errorf("escribiendo %s: %w", p.file, err)
	}

	if err := loadQuarantine(ctx, texto); err != nil {
		return err
	}
	p.log.Info("cuarentena de base puesta", "reglas", len(rules), "fichero", p.file)
	return nil
}

// quarantineScript arma el fichero `.nft` de la cuarentena.
//
// # Por qué texto y no netlink, que es lo que usa la compuerta
//
// Porque este fichero tiene que poder cargarlo `nftables.service` al arrancar,
// sin Kanpachi corriendo. Eso solo se hace con un `.nft`, así que el texto es el
// artefacto y generar netlink además sería tener dos fuentes de la misma verdad.
//
// # Por qué empieza declarando la tabla vacía y vaciándola
//
// Es el modismo de nftables para reemplazar en vez de acumular. La declaración
// vacía crea la tabla si falta y no hace nada si está; el `flush` la deja limpia;
// y lo de abajo la vuelve a llenar.
//
// Sin esas dos líneas, cargar el fichero dos veces DUPLICA todas las reglas.
// Medido el 2026-08-10 en el banco: 48 reglas tras la primera carga y 96 tras la
// segunda, y el daemon la carga en cada arranque. El comentario que decía que
// era re-ejecutable ya estaba escrito acá antes de que el código lo hiciera.
//
// # Esto se parece a "purgar y reescribir", que está prohibido, y no lo es
//
// El guardián de `internal/arch` existe porque reponer la cuarentena purgando y
// reescribiendo abre una ventana sin protección en cada arranque del servicio, y
// si el arranque falla a la mitad la ventana no se cierra nunca.
//
// Acá no hay ventana: `nft -f` aplica el fichero ENTERO en una transacción. El
// vaciado y el relleno se publican juntos o no se publica ninguno. Esa garantía
// es de nftables y es justo lo que la API de COM del firewall de Windows no
// ofrece, que es por lo que allá el mismo patrón sí sería el fallo.
func quarantineScript(rules []domain.QuarantineRule) (string, error) {
	orden := append([]domain.QuarantineRule(nil), rules...)
	sort.Slice(orden, func(i, j int) bool { return orden[i].Name < orden[j].Name })

	var b strings.Builder
	b.WriteString("# Generado por Kanpachi. No editar a mano: se regenera entero.\n")
	b.WriteString("#\n")
	b.WriteString("# La cuarentena de base cierra puertos peligrosos del sistema hacia\n")
	b.WriteString("# cualquier red, y sigue puesta con Kanpachi apagado. Eso es lo que\n")
	b.WriteString("# impide que apagar el producto reabra lo que el producto cerró.\n")
	b.WriteString("#\n")
	b.WriteString("# Se carga sola al arrancar. Para quitarla: nft delete table inet " +
		QuarantineTable + "\n\n")

	// Reemplazar y no acumular. Ver la explicación arriba: sin estas dos líneas,
	// cargar dos veces duplica todo, y `nft -f` las aplica junto con el resto en
	// una sola transacción, así que no hay instante sin cuarentena.
	fmt.Fprintf(&b, "table inet %s { }\n", QuarantineTable)
	fmt.Fprintf(&b, "flush table inet %s\n\n", QuarantineTable)

	fmt.Fprintf(&b, "table inet %s {\n", QuarantineTable)
	fmt.Fprintf(&b, "\tchain input {\n")
	// Prioridad por delante del filtro normal. Un `drop` acá corta la evaluación
	// de todo lo demás, así que la cuarentena le gana a cualquier `accept` que
	// alguien tenga más abajo, incluido el nuestro.
	fmt.Fprintf(&b, "\t\ttype filter hook input priority filter - 10; policy accept;\n")
	if err := writeRules(&b, orden, true); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "\tchain output {\n")
	fmt.Fprintf(&b, "\t\ttype filter hook output priority filter - 10; policy accept;\n")
	if err := writeRules(&b, orden, false); err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "\t}\n")
	fmt.Fprintf(&b, "}\n")
	return b.String(), nil
}

// writeRules vuelca las reglas de un sentido.
//
// El puerto se compara siempre como LOCAL, igual que en Windows: en entrada es
// el de destino y en salida el de origen. Compararlo al revés convertiría el
// bloqueo de "que nadie llegue a mi SMB" en "que yo no pueda usar el SMB de
// nadie", que es romperle la máquina al usuario sin protegerlo de nada.
func writeRules(b *strings.Builder, rules []domain.QuarantineRule, in bool) error {
	for _, r := range rules {
		if r.In != in {
			continue
		}
		proto, err := protoName(r.Proto)
		if err != nil {
			return fmt.Errorf("regla %q: %w", r.Name, err)
		}
		campo := "sport"
		if in {
			campo = "dport"
		}
		puerto := fmt.Sprintf("%d", r.From)
		if r.From != r.To {
			puerto = fmt.Sprintf("%d-%d", r.From, r.To)
		}
		fmt.Fprintf(b, "\t\t%s %s %s drop comment %q\n", proto, campo, puerto, r.Name)
	}
	return nil
}

func protoName(p domain.Proto) (string, error) {
	switch p {
	case domain.ProtoTCP:
		return "tcp", nil
	case domain.ProtoUDP:
		return "udp", nil
	default:
		return "", fmt.Errorf("protocolo %v: la cuarentena se escribe por protocolo concreto", p)
	}
}
