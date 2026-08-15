// Package igd le pregunta al router del usuario qué puertos tiene abiertos.
//
// # Es la excepción de SOLO LECTURA a "el router no se toca nunca"
//
// `port.ExposureAudit.RouterMappings` lo dice y este paquete lo cumple por
// construcción: **no hay ninguna función que cree ni que borre un mapeo**. Esa
// ausencia es el punto. Kanpachi existe para que nadie tenga que abrir puertos
// en su router, y lo único que hace acá es mirar si alguien ya los abrió, casi
// siempre un juego o un instalador de hace años que nadie recuerda.
//
// # Por qué se le permite fallar, y qué NO se le permite
//
// Un router que no contesta es lo normal: hay modelos con UPnP apagado de
// fábrica, redes de residencia sin IGD, y CGNAT. Nada de eso impide crear una
// sala, así que un fallo acá se convierte en una alerta y jamás en un error
// fatal.
//
// Lo que no se le permite es tardar. Todo el camino va contra un plazo, porque
// esto se llama desde el mismo sitio que dibuja una pantalla.
//
// # El cinturón, que acá sí hace falta
//
// Este es el ÚNICO punto del producto que hace una petición HTTP a una
// dirección que decide un tercero: la trae la cabecera `LOCATION` de una
// respuesta a un multicast, o sea que la escribe quien quiera contestar en la
// LAN. Y el daemon corre como SYSTEM. Sin acotarlo, eso es una falsificación de
// petición desde el servidor, con el sistema operativo detrás. Por eso:
//
//   - La URL tiene que apuntar EXACTAMENTE al equipo que contestó. Cualquier
//     otro destino se descarta sin pedirlo.
//   - Ese equipo tiene que estar en una dirección privada o de enlace local.
//   - No se siguen redirecciones: una de ellas anularía las dos reglas de
//     arriba después de haberlas comprobado.
//   - Los cuerpos se leen acotados, y la cantidad de mapeos también.
package igd

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/accentiostudios/kanpachi/core/domain"
	"github.com/accentiostudios/kanpachi/core/port"
	"github.com/accentiostudios/kanpachi/core/timing"
)

// Los topes que no se miden en tiempo. Los plazos viven en [timing], porque
// salen de que esto alimenta una pantalla y hay que poder compararlos con el
// resto de lo que el usuario espera.
const (
	// maxBody es lo que se lee de una respuesta. La descripción de un IGD son
	// unos pocos kilobytes.
	maxBody = 256 << 10
	// maxMappings acota el recorrido. Un router doméstico tiene decenas; el
	// tope está para que uno que conteste siempre lo mismo no deje esto dando
	// vueltas para siempre.
	maxMappings = 128
)

// ssdpTarget es la dirección del multicast de descubrimiento, y el tipo de
// dispositivo que se busca.
const (
	ssdpAddr   = "239.255.255.250:1900"
	ssdpDevice = "urn:schemas-upnp-org:device:InternetGatewayDevice:1"
)

// Los servicios que saben contestar por los mapeos, en orden de preferencia.
var mappingServices = []string{
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// Router pregunta por los mapeos del IGD.
type Router struct {
	log port.Logger
}

func New(log port.Logger) *Router { return &Router{log: log} }

// FirewallEnabled no es cosa de este adaptador.
//
// [port.ExposureAudit] tiene tres preguntas y este objeto solo contesta una. La
// composición la hace `cmd/kanpachid`, que es donde este binario decide con
// qué; acá se devuelve error en vez de `nil, nil` por el mismo motivo que el
// firewall se niega a contestar por el router: "no hay nada" y "nadie miró"
// tienen que poder distinguirse en la única pantalla cuyo trabajo es eso.
func (r *Router) FirewallEnabled(context.Context) ([]domain.FirewallProfileState, error) {
	return nil, fmt.Errorf("igd: el estado del firewall lo mide el adaptador del firewall")
}

// Enforcement tampoco. Mismo motivo que [Router.FirewallEnabled].
func (r *Router) Enforcement(context.Context) (domain.Enforcement, error) {
	return domain.Enforcement{}, fmt.Errorf("igd: la contención puesta la mide el adaptador del firewall")
}

// RouterMappings devuelve los reenvíos de puerto que el router tiene puestos.
func (r *Router) RouterMappings(ctx context.Context) ([]domain.PortMapping, error) {
	ctx, cancel := context.WithTimeout(ctx, timing.IGDWholeBudget)
	defer cancel()

	loc, from, err := discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("buscando el router: %w", err)
	}

	controlURL, service, err := r.controlURL(ctx, loc, from)
	if err != nil {
		return nil, fmt.Errorf("leyendo la descripción del router: %w", err)
	}

	mappings, err := r.readMappings(ctx, controlURL, service)
	if err != nil {
		return nil, err
	}
	r.log.Info("mapeos del router leídos", "cantidad", len(mappings), "service", service)
	return mappings, nil
}

// discover manda el M-SEARCH y devuelve la primera URL de descripción válida,
// junto con la dirección del equipo que la mandó.
//
// La dirección de origen se devuelve porque es la mitad del cinturón: sin ella
// no hay contra qué comprobar la URL.
func discover(ctx context.Context) (*url.URL, netip.Addr, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, netip.Addr{}, fmt.Errorf("abriendo el socket de descubrimiento: %w", err)
	}
	defer func() { _ = conn.Close() }()

	target, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return nil, netip.Addr{}, err
	}

	// MX es cuántos segundos puede esperar el que contesta antes de hacerlo, y
	// existe para que cien dispositivos no contesten a la vez. Va bajo porque
	// solo interesa uno.
	query := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: " + ssdpAddr + "\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 1\r\n" +
		"ST: " + ssdpDevice + "\r\n\r\n"

	deadline := time.Now().Add(timing.IGDSearchWait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, netip.Addr{}, err
	}

	// Dos envíos, separados. El multicast se pierde sin avisar y sin acuse, y
	// repetir una vez cuesta nada.
	for i := 0; i < 2; i++ {
		if _, err := conn.WriteTo([]byte(query), target); err != nil {
			return nil, netip.Addr{}, fmt.Errorf("mandando el M-SEARCH: %w", err)
		}
		time.Sleep(150 * time.Millisecond)
	}

	buf := make([]byte, 4096)
	for {
		n, sender, err := conn.ReadFrom(buf)
		if err != nil {
			return nil, netip.Addr{}, fmt.Errorf("nadie contestó al descubrimiento: %w", err)
		}
		origin, ok := addrOf(sender)
		if !ok {
			continue
		}
		loc := locationHeader(string(buf[:n]))
		if loc == "" {
			continue
		}
		u, err := url.Parse(loc)
		if err != nil {
			continue
		}
		if !sameHost(u, origin) {
			// Alguien contestó apuntando a otro sitio. Se descarta y se sigue
			// escuchando: puede haber una respuesta buena detrás.
			continue
		}
		return u, origin, nil
	}
}

// locationHeader saca la cabecera LOCATION de una respuesta SSDP.
func locationHeader(resp string) string {
	sc := bufio.NewScanner(strings.NewReader(resp))
	for sc.Scan() {
		line := sc.Text()
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(line[:i]), "LOCATION") {
			continue
		}
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

// addrOf saca la IP de una dirección de paquete.
func addrOf(a net.Addr) (netip.Addr, bool) {
	ua, ok := a.(*net.UDPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	ip, ok := netip.AddrFromSlice(ua.IP)
	if !ok {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

// sameHost es el cinturón: la URL tiene que apuntar al equipo que contestó, y
// ese equipo tiene que estar en la red local.
//
// Las dos mitades hacen falta. Sin la primera, cualquiera de la LAN manda a
// Kanpachi a pedir una URL arbitraria. Sin la segunda, una respuesta from
// out de la red local convertiría esto en un client de internet.
func sameHost(u *url.URL, origin netip.Addr) bool {
	if u.Scheme != "http" {
		return false
	}
	if !origin.IsPrivate() && !origin.IsLinkLocalUnicast() && !origin.IsLoopback() {
		return false
	}
	host := u.Hostname()
	ip, err := netip.ParseAddr(host)
	if err != nil {
		// Un nombre y no una IP. No se resuelve: resolverlo abriría justo el
		// agujero que estas comprobaciones cierran.
		return false
	}
	return ip.Unmap() == origin
}

// controlURL lee la descripción del dispositivo y saca de ahí a quién
// preguntarle por los mapeos.
func (r *Router) controlURL(ctx context.Context, loc *url.URL, origin netip.Addr) (*url.URL, string, error) {
	body, err := r.get(ctx, loc)
	if err != nil {
		return nil, "", err
	}

	var root rootDoc
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, "", fmt.Errorf("la descripción no es XML que se entienda: %w", err)
	}

	var all []serviceDoc
	collectServices(&root.Device, &all)

	for _, want := range mappingServices {
		for _, s := range all {
			if !strings.EqualFold(strings.TrimSpace(s.ServiceType), want) {
				continue
			}
			u, err := loc.Parse(strings.TrimSpace(s.ControlURL))
			if err != nil {
				continue
			}
			// La URL de controlURL la escribe el mismo documento que vino de la
			// red, así que pasa por el mismo filtro que la de descripción.
			if !sameHost(u, origin) {
				continue
			}
			return u, want, nil
		}
	}
	return nil, "", fmt.Errorf("el router no publica ningún servicio de mapeo de puertos")
}

// readMappings recorre los mapeos por índice hasta que el router dice que no
// hay más.
func (r *Router) readMappings(ctx context.Context, controlURL *url.URL, service string) ([]domain.PortMapping, error) {
	var out []domain.PortMapping
	for i := 0; i < maxMappings; i++ {
		if err := ctx.Err(); err != nil {
			// Lo leído hasta acá vale: son mappings de verdad, y devolverlos con
			// el aviso es mejor que tirar el trabajo por un plazo.
			r.log.Warn("se acabó el plazo leyendo los mapeos", "leídos", len(out))
			return out, nil
		}
		m, ok, err := r.mappingAt(ctx, controlURL, service, i)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("preguntando por el primer mapeo: %w", err)
			}
			// A mitad del recorrido, un error es la forma que tienen muchos
			// routers de decir "no hay más": devuelven un fallo SOAP en vez de
			// una lista vacía.
			return out, nil
		}
		if !ok {
			return out, nil
		}
		out = append(out, m)
	}
	r.log.Warn("el router publica más mappings que el tope y se cortó", "tope", maxMappings)
	return out, nil
}

// mappingAt pide un mapeo por su índice.
func (r *Router) mappingAt(ctx context.Context, controlURL *url.URL, service string, i int) (domain.PortMapping, bool, error) {
	envelope := `<?xml version="1.0"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"` +
		` s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>` +
		`<u:GetGenericPortMappingEntry xmlns:u="` + service + `">` +
		`<NewPortMappingIndex>` + strconv.Itoa(i) + `</NewPortMappingIndex>` +
		`</u:GetGenericPortMappingEntry></s:Body></s:Envelope>`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, controlURL.String(), strings.NewReader(envelope))
	if err != nil {
		return domain.PortMapping{}, false, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+service+`#GetGenericPortMappingEntry"`)

	body, status, err := r.do(req)
	if err != nil {
		return domain.PortMapping{}, false, err
	}
	if status != http.StatusOK {
		// 500 con un fallo SOAP dentro es como el IGD dice que ese índice no
		// existe, que es el final normal del recorrido.
		return domain.PortMapping{}, false, nil
	}

	var resp mappingResp
	if err := xml.Unmarshal(body, &resp); err != nil {
		return domain.PortMapping{}, false, fmt.Errorf("la respuesta del router no se entendió: %w", err)
	}
	e := resp.Body.Entry

	external, err := strconv.ParseUint(strings.TrimSpace(e.ExternalPort), 10, 16)
	if err != nil {
		return domain.PortMapping{}, false, nil
	}
	internal, err := strconv.ParseUint(strings.TrimSpace(e.InternalPort), 10, 16)
	if err != nil {
		return domain.PortMapping{}, false, nil
	}
	proto := domain.ProtoTCP
	if strings.EqualFold(strings.TrimSpace(e.Protocol), "UDP") {
		proto = domain.ProtoUDP
	}
	client, _ := netip.ParseAddr(strings.TrimSpace(e.InternalClient))

	return domain.PortMapping{
		Proto:        proto,
		ExternalPort: uint16(external),
		InternalIP:   client,
		InternalPort: uint16(internal),
		Description:  strings.TrimSpace(e.Description),
	}, true, nil
}

// get trae un documento con su plazo y su tope de tamaño.
func (r *Router) get(ctx context.Context, u *url.URL) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	body, status, err := r.do(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("el router contestó %d", status)
	}
	return body, nil
}

// do es el único sitio que habla HTTP, y trae puesto el cinturón entero.
func (r *Router) do(req *http.Request) ([]byte, int, error) {
	client := &http.Client{
		Timeout: timing.IGDHTTPTimeout,
		// Sin redirecciones. Seguir una anularía la comprobación de target
		// justo después de haberla hecho.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// Los documentos, tal cual vienen. Se declara lo que se lee y nada más: un
// campo de más es un campo que hay que decidir si es de fiar.

type rootDoc struct {
	XMLName xml.Name  `xml:"root"`
	Device  deviceDoc `xml:"device"`
}

type deviceDoc struct {
	DeviceType string       `xml:"deviceType"`
	Services   []serviceDoc `xml:"serviceList>service"`
	Devices    []deviceDoc  `xml:"deviceList>device"`
}

type serviceDoc struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

// collectServices baja por el árbol de dispositivos.
//
// Recursivo porque el servicio que interesa vive dos niveles abajo del root,
// dentro de un WANDevice y de un WANConnectionDevice, y no all los routers
// respetan esa profundidad.
func collectServices(d *deviceDoc, dst *[]serviceDoc) {
	*dst = append(*dst, d.Services...)
	for i := range d.Devices {
		collectServices(&d.Devices[i], dst)
	}
}

type mappingResp struct {
	Body struct {
		Entry struct {
			RemoteHost     string `xml:"NewRemoteHost"`
			ExternalPort   string `xml:"NewExternalPort"`
			Protocol       string `xml:"NewProtocol"`
			InternalPort   string `xml:"NewInternalPort"`
			InternalClient string `xml:"NewInternalClient"`
			Enabled        string `xml:"NewEnabled"`
			Description    string `xml:"NewPortMappingDescription"`
		} `xml:"GetGenericPortMappingEntryResponse"`
	} `xml:"Body"`
}

var _ port.ExposureAudit = (*Router)(nil)
