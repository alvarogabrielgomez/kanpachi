; El instalador de Kanpachi.
;
; Compilar con Inno Setup 6:
;
;   .\scripts\preparar-carga.ps1
;   ISCC.exe installer\kanpachi.iss
;
; NOTA DE ESTADO: este script esta escrito y NO esta medido. Esta maquina no
; tiene Inno Setup instalado, asi que nunca se compilo ni se ejecuto. Lo que si
; esta medido es la carga que empaqueta, que la arma preparar-carga.ps1. El
; criterio de aceptacion de docs/04 sigue siendo el de siempre: instalar y
; desinstalar veinte veces seguidas en una VM sin dejar rastro.

#define AppName        "Kanpachi"
#define AppVersion     "0.1.0"
#define AppPublisher   "Accentio Studios"
#define ServiceName    "kanpachi-daemon"
#define DaemonExe      "kanpachid.exe"
#define UiExe          "Kanpachi.exe"
#define Carga          "C:\kt\carga"

[Setup]
AppId={{7A6C1D2E-4B3F-4E1A-9C5D-0A1B2C3D4E5F}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputBaseFilename=kanpachi-setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; Los dos son de 64 bits y el daemon habla con APIs que no existen en WOW64.
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; EL UNICO UAC DE LA VIDA DEL PRODUCTO. Todo lo que hace falta elevar se hace
; aca: registrar el servicio, crear ProgramData con su ACL, y conceder el
; arranque al usuario. Despues de esto, ni al encender la PC ni al abrir la app.
PrivilegesRequired=admin
; Un desinstalador que NO puede correrse a medias: deja puertos bloqueados.
UninstallDisplayIcon={app}\{#UiExe}

[Languages]
Name: "es"; MessagesFile: "compiler:Languages\Spanish.isl"

[Files]
; La carga ENTERA, tal como la dejo preparar-carga.ps1. Sin lista de ficheros a
; mano: el bundle de Flutter son veintitantos y una lista se desincroniza en
; silencio con la primera dependencia nueva.
Source: "{#Carga}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
; **Los accesos directos apuntan al DAEMON, no a la interfaz.**
;
; El daemon es lo que Kanpachi ES; la interfaz son sus mandos. Quien lanza la
; interfaz es siempre el daemon, asi que un acceso directo a la interfaz podria
; dejar mandos abiertos sin nada detras. Ver el modelo de procesos en docs/03.
;
; El icono se toma del ejecutable de la interfaz, que es el que lo tiene.
Name: "{group}\{#AppName}"; Filename: "{app}\{#DaemonExe}"; IconFilename: "{app}\{#UiExe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#DaemonExe}"; IconFilename: "{app}\{#UiExe}"

[Registry]
; El manejador de kanpachi://, que es como entra un enlace de invitacion.
;
; Abre la INTERFAZ y no el daemon: quien sabe leer un codigo de sala es la
; interfaz, y si Kanpachi ya esta corriendo se topa con la instancia unica, le
; pasa el enlace a la ventana que ya hay, y se muere.
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi"; ValueType: string; ValueName: ""; ValueData: "URL:Kanpachi"; Flags: uninsdeletekey
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: """{app}\{#UiExe}"",0"
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#UiExe}"" ""%1"""

[Run]
Filename: "{app}\{#DaemonExe}"; Description: "Abrir Kanpachi"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; **El orden importa y es lo contrario de lo intuitivo.**
;
; Primero se DETIENE el servicio, luego se corre la limpieza con los ficheros
; todavia en disco, y solo despues se borra el servicio. Al reves, la limpieza
; no tendria binario que ejecutar y la maquina se quedaria con la cuarentena de
; base puesta: bloqueos sobre el 445 y el 3389 en toda la PC, con Kanpachi ya
; borrado, sin causa visible y sin nada que culpar.
Filename: "{sys}\sc.exe"; Parameters: "stop {#ServiceName}"; Flags: runhidden; RunOnceId: "PararServicio"
Filename: "{app}\{#DaemonExe}"; Parameters: "--uninstall-cleanup"; Flags: runhidden waituntilterminated; RunOnceId: "LimpiarTodo"
Filename: "{sys}\sc.exe"; Parameters: "delete {#ServiceName}"; Flags: runhidden; RunOnceId: "BorrarServicio"

[UninstallDelete]
; ProgramData no lo gestiona Inno porque no lo creo Inno.
Type: filesandordirs; Name: "{commonappdata}\{#AppName}"

[Code]
{ Corre un programa oculto y devuelve su codigo de salida. }
function Correr(const Exe, Params: string; var Codigo: Integer): Boolean;
begin
  Result := Exec(Exe, Params, '', SW_HIDE, ewWaitUntilTerminated, Codigo);
end;

function SysExe(const Nombre: string): string;
begin
  Result := ExpandConstant('{sys}\') + Nombre;
end;

{ Registra el servicio, su politica de recuperacion, y la concesion al usuario. }
procedure RegistrarServicio();
var
  Codigo: Integer;
  Bin: string;
begin
  Bin := ExpandConstant('{app}\{#DaemonExe}');

  { Arranque automatico RETRASADO. Un servicio que toca el firewall y levanta un
    adaptador durante el arranque de Windows compite con todo lo demas;
    retrasado llega cuando la maquina ya respira, y nadie esta jugando en el
    segundo cero. Los espacios despues de los "=" son sintaxis de sc.exe. }
  Correr(SysExe('sc.exe'),
    'create {#ServiceName} binPath= "' + Bin + '" start= delayed-auto DisplayName= "Kanpachi"',
    Codigo);

  Correr(SysExe('sc.exe'),
    'description {#ServiceName} "Abre y cierra los puertos de tus partidas en LAN, y los vuelve a cerrar al salir."',
    Codigo);

  { Reintentar a los 5 s, 10 s y 30 s, y contar desde cero pasado un dia. Si el
    daemon se cae, la sala se cae con el: volver solo es la diferencia entre una
    partida interrumpida y una partida perdida. }
  Correr(SysExe('sc.exe'),
    'failure {#ServiceName} reset= 86400 actions= restart/5000/restart/10000/restart/30000',
    Codigo);

  { **La concesion minima, y es lo que mantiene la promesa de un solo UAC.**

    Con el daemon parado, hacer doble clic en el acceso directo tiene que poder
    arrancar el servicio, y arrancar un servicio exige permiso. Se le concede al
    usuario interactivo sobre ESTE servicio y ninguno mas.

    El descriptor es el de por defecto de un servicio con UNA diferencia: la
    entrada de IU (usuarios interactivos) pasa de CCLCSWLOCRRC a
    CCLCSWRPWPLOCRRC, o sea gana RP (arrancar) y WP (detener). Todo lo demas se
    escribe igual porque `sc sdset` REEMPLAZA el descriptor entero: omitir las
    entradas de SYSTEM o de Administradores dejaria un servicio que ni el
    sistema puede manejar.

    Lo que NO se concede es CCDC ni WD/WO: el usuario no puede reconfigurar ni
    borrar el servicio. El interruptor de "abrir con Windows" no pasa por aqui;
    lo hace el daemon consigo mismo, que ya es SYSTEM. }
  Correr(SysExe('sc.exe'),
    'sdset {#ServiceName} ' +
    'D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)' +
    '(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)' +
    '(A;;CCLCSWRPWPLOCRRC;;;IU)' +
    '(A;;CCLCSWLOCRRC;;;SU)' +
    'S:(AU;FA;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;WD)',
    Codigo);
end;

{ Crea ProgramData\Kanpachi con su ACL propia.

  **La ACL es la mitad de la proteccion del token de la API local**, y por eso
  el daemon se NIEGA a crear este directorio: crearlo el heredaria la ACL de
  ProgramData y la perderia en silencio.

  Escritura solo para SYSTEM y Administradores. LECTURA para los usuarios de la
  maquina, y eso es deliberado: la interfaz corre sin elevar y tiene que poder
  leer api.token. Lo que acota la superficie no es este permiso, es la lista
  cerrada de metodos del protocolo.

  /inheritance:r es la mitad que se olvida: sin ella la ACL nueva se SUMA a la
  heredada y esto no habria servido de nada. }
procedure CrearDirectorioDeDatos();
var
  Codigo: Integer;
  Datos: string;
begin
  Datos := ExpandConstant('{commonappdata}\{#AppName}');
  if not DirExists(Datos) then
    CreateDir(Datos);

  Correr(SysExe('icacls.exe'), '"' + Datos + '" /inheritance:r', Codigo);
  Correr(SysExe('icacls.exe'), '"' + Datos + '" /grant "*S-1-5-18":(OI)(CI)F', Codigo);
  Correr(SysExe('icacls.exe'), '"' + Datos + '" /grant "*S-1-5-32-544":(OI)(CI)F', Codigo);
  Correr(SysExe('icacls.exe'), '"' + Datos + '" /grant "*S-1-5-32-545":(OI)(CI)RX', Codigo);
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  Codigo: Integer;
begin
  if CurStep = ssPostInstall then
  begin
    CrearDirectorioDeDatos();
    RegistrarServicio();
    { El servicio NO se arranca aca. Lo arranca el acceso directo del paso
      [Run], que ademas le pasa que abra la ventana. Arrancarlo por las dos vias
      dejaria un daemon corriendo en silencio y un segundo intento de arranque
      que no hace nada. }
    Codigo := 0;
  end;
end;

function InitializeUninstall(): Boolean;
begin
  { Se avisa antes de tocar nada. Desinstalar cierra la sala de todos los que
    esten dentro, y eso no puede ser una sorpresa. }
  Result := MsgBox('Se va a desinstalar Kanpachi.' + #13#10#13#10 +
    'Si hay una sala abierta se cerrara, y los puertos que Kanpachi tenia ' +
    'bloqueados para protegerte volveran a su estado anterior.' + #13#10#13#10 +
    'Continuar?', mbConfirmation, MB_YESNO) = IDYES;
end;
