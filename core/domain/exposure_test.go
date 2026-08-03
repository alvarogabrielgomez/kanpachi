package domain

import "testing"

// El agujero que este archivo vigila, dicho una vez:
//
// `AuditForeign` buscaba por la ruta del ejecutable DEL JUEGO ACTIVO, así que
// una regla permisiva de Parsec o de Sunshine no se miraba nunca. La cuarentena
// tapa el escritorio remoto estándar por puerto (3389, 5985, 5986, 445), y esas
// herramientas escuchan donde el usuario les diga, así que ninguna lista de
// puertos las cubre. Lo único estable es el ejecutable.

func TestElControlRemotoSeReconocePorElNombreDelArchivo(t *testing.T) {
	casos := []struct {
		nombre string
		exe    string
		juego  string
		quiere RuleClass
		porQué string
	}{
		{
			nombre: "parsec con ruta completa de windows",
			exe:    `C:\Program Files\Parsec\parsecd.exe`,
			quiere: ClassRemoteControl,
			porQué: "la ruta depende de dónde lo instaló cada uno, así que se compara el nombre",
		},
		{
			nombre: "mayúsculas mezcladas",
			exe:    `C:\Program Files\Sunshine\SUNSHINE.EXE`,
			quiere: ClassRemoteControl,
			porQué: "el almacén de reglas devuelve lo que escribió el instalador",
		},
		{
			nombre: "barra normal",
			exe:    "D:/juegos/rustdesk.exe",
			quiere: ClassRemoteControl,
			porQué: "una regla puede traer cualquiera de las dos barras",
		},
		{
			nombre: "sin ruta, solo el nombre",
			exe:    "anydesk.exe",
			quiere: ClassRemoteControl,
		},
		{
			nombre: "con espacios alrededor",
			exe:    "  moonlight.exe  ",
			quiere: ClassRemoteControl,
		},
		{
			nombre: "el ejecutable del juego activo",
			exe:    `C:\Steam\steamapps\common\Zomboid\ProjectZomboid64.exe`,
			juego:  `C:\Steam\steamapps\common\Zomboid\ProjectZomboid64.exe`,
			quiere: ClassGame,
		},
		{
			nombre: "el juego, con el instalador escribiendo otra ruta",
			exe:    `c:\steam\steamapps\common\zomboid\projectzomboid64.exe`,
			juego:  `C:\Steam\SteamApps\Common\Zomboid\ProjectZomboid64.exe`,
			quiere: ClassGame,
			porQué: "Windows no distingue mayúsculas en rutas",
		},
		{
			nombre: "cualquier otra cosa",
			exe:    `C:\Program Files\Spotify\Spotify.exe`,
			juego:  `C:\juegos\zomboid.exe`,
			quiere: ClassOther,
		},
		{
			nombre: "sin ejecutable",
			exe:    "",
			quiere: ClassOther,
			porQué: "una regla de puerto suelto no tiene ejecutable y no es clasificable",
		},
		{
			nombre: "el control remoto gana sobre el juego",
			exe:    `C:\Parsec\parsecd.exe`,
			juego:  `C:\Parsec\parsecd.exe`,
			quiere: ClassRemoteControl,
			porQué: "si estuviera en las dos listas, hay que tratarlo como lo peor de las dos",
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if got := ClassifyForeign(c.exe, c.juego); got != c.quiere {
				t.Errorf("ClassifyForeign(%q, %q) = %v, quiere %v\n  %s",
					c.exe, c.juego, got, c.quiere, c.porQué)
			}
		})
	}
}

func TestSoloElControlRemotoBloquea(t *testing.T) {
	reglas := []ForeignRule{
		{Name: "juego", Class: ClassGame},
		{Name: "parsec", Class: ClassRemoteControl},
		{Name: "otra", Class: ClassOther},
		{Name: "sunshine", Class: ClassRemoteControl},
	}

	bloq := BlockingForeign(reglas)
	if len(bloq) != 2 {
		t.Fatalf("bloquean %d, quiere 2: %+v", len(bloq), bloq)
	}
	for _, r := range bloq {
		if r.Class != ClassRemoteControl {
			t.Errorf("%q bloquea con clase %v, y solo el control remoto bloquea", r.Name, r.Class)
		}
	}

	// Una regla de juego se muestra y se ofrece suspender. Nunca detiene la
	// sala, porque hacerlo convertiría cada instalador ruidoso en un producto
	// que no arranca.
	if (ForeignRule{Class: ClassGame}).Blocking() {
		t.Error("una regla de juego no puede bloquear la apertura de la sala")
	}
}

func TestLaListaDeControlRemotoEsUnaCopia(t *testing.T) {
	a := RemoteAccessExecutables()
	if len(a) == 0 {
		t.Fatal("la lista está vacía, así que el agujero de Parsec sigue abierto")
	}
	a[0] = "manoseado.exe"

	b := RemoteAccessExecutables()
	if b[0] == "manoseado.exe" {
		t.Error("quien la pida puede alterar la política del dominio")
	}
	if ClassifyForeign(remoteAccessExes[0], "") != ClassRemoteControl {
		t.Error("clasificar dejó de funcionar después de que alguien tocara la copia")
	}
}

func TestTodaLaListaSeReconoceYEstaEnMinusculas(t *testing.T) {
	for _, exe := range RemoteAccessExecutables() {
		if exe != baseLower(exe) {
			t.Errorf("%q tiene que estar en minúsculas y sin ruta, porque así se compara", exe)
		}
		if got := ClassifyForeign(`C:\donde sea\`+exe, ""); got != ClassRemoteControl {
			t.Errorf("%q se clasificó como %v", exe, got)
		}
	}
}

func TestLaClaseSeNombraEnCastellano(t *testing.T) {
	for _, c := range []RuleClass{ClassGame, ClassRemoteControl, ClassOther} {
		if s := c.String(); s == "" || s == "clase-inválida" {
			t.Errorf("la clase %d se muestra como %q", c, s)
		}
	}
	if s := RuleClass(0).String(); s != "clase-inválida" {
		t.Errorf("el cero se muestra como %q, y tiene que verse que está mal", s)
	}
}
