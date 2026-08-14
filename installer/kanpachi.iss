; Instalador de Kanpachi. Compilar:
;
;   .\scripts\prepare-payload.ps1 -Output .\dist\carga
;   ISCC.exe /DCarga=".\dist\carga" installer\kanpachi.iss
;
; El criterio de aceptacion completo sigue siendo el de docs/04: instalar y
; desinstalar veinte veces en una VM sin dejar rastro.

#define AppName        "Kanpachi"
#ifndef AppVersion
  #define AppVersion   "0.0.0"
#endif
; VersionInfoVersion tiene que ser numerica: un 0.2.0-rc1 pierde el sufijo aqui
; y solo aqui. Se pasa con /DVersionInfo=...
#ifndef VersionInfo
  #define VersionInfo  "0.0.0"
#endif
#define AppPublisher   "Accentio Studios"
; El repositorio llega POR PARAMETRO, igual que la version, y no escrito aca.
;
; Inno Setup no puede importar la constante de Go, y una copia mas de esa cadena
; es una que un fork edita en un sitio y se olvida en otro: lo vigila
; `internal/arch/marca_test.go`. El respaldo es para compilar a mano, donde no
; hay canal que respetar porque ese instalador no se publica.
#ifndef Repo
  #define Repo "ejemplo/kanpachi"
#endif
#define AppURL         "https://github.com/" + Repo
#define ServiceName    "kanpachi-daemon"
#define DaemonExe      "kanpachid.exe"
#define UiExe          "kanpachiui.exe"
; Lanzador + abrir ventana. Mismo binario, papel distinto: ver docs/03.
#define ArgShow        "--show"
; La carga que arma prepare-payload.ps1. Se pasa con /DCarga=...
#ifndef Carga
  #define Carga        "..\dist\carga"
#endif

[Setup]
AppId={{7A6C1D2E-4B3F-4E1A-9C5D-0A1B2C3D4E5F}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
VersionInfoVersion={#VersionInfo}
DefaultDirName={autopf}\{#AppName}
DefaultGroupName={#AppName}
DisableProgramGroupPage=yes
OutputDir=..\dist
; Sin la version en el nombre, a proposito: la pagina de descarga apunta a
; releases/latest/download/kanpachi-setup.exe, que es una URL permanente. Con el
; nombre versionado habria que editar la pagina en cada publicacion, y la pagina
; que se edita a mano es la que se queda vieja. La version va dentro del .exe.
OutputBaseFilename=kanpachi-setup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
; 64 bits: el daemon usa APIs que no existen en WOW64.
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
; EL UNICO UAC DE LA VIDA DEL PRODUCTO. Todo lo que hay que elevar se hace aca:
; el servicio, ProgramData con su ACL, y la concesion de arranque al usuario.
PrivilegesRequired=admin
; Un desinstalador a medias deja puertos bloqueados.
UninstallDisplayIcon={app}\{#UiExe}

[Languages]
Name: "es"; MessagesFile: "compiler:Languages\Spanish.isl"

[Files]
; La carga entera. Sin lista a mano: el bundle de Flutter son veintitantos
; ficheros y una lista se desincroniza con la primera dependencia nueva.
Source: "{#Carga}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs

[Icons]
; Apuntan al DAEMON. La interfaz sin daemon son mandos sin nada detras, y a un
; servicio no lo arranca un doble clic: lo pide este binario con --show.
Name: "{group}\{#AppName}"; Filename: "{app}\{#DaemonExe}"; Parameters: "{#ArgShow}"; IconFilename: "{app}\{#UiExe}"
Name: "{autodesktop}\{#AppName}"; Filename: "{app}\{#DaemonExe}"; Parameters: "{#ArgShow}"; IconFilename: "{app}\{#UiExe}"

[Registry]
; El manejador de kanpachi://, por donde entra un enlace de invitacion.
;
; El "%1" es el enlace. Va al daemon, que lo guarda y abre la ventana; la
; interfaz lo recoge con pending_invite y pide confirmacion. Nada que llegue de
; fuera entra a una sala sin que alguien lo confirme dentro.
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi"; ValueType: string; ValueName: ""; ValueData: "URL:Kanpachi"; Flags: uninsdeletekey
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: """{app}\{#UiExe}"",0"
Root: HKLM; Subkey: "SOFTWARE\Classes\kanpachi\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#DaemonExe}"" {#ArgShow} ""%1"""

[Run]
Filename: "{app}\{#DaemonExe}"; Parameters: "{#ArgShow}"; Description: "Abrir Kanpachi"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; El orden es lo contrario de lo intuitivo: parar, limpiar CON los ficheros
; todavia en disco, y despues borrar el servicio. Al reves la limpieza no tiene
; binario que ejecutar y la maquina se queda con la cuarentena de base puesta:
; el 445 y el 3389 bloqueados, sin Kanpachi y sin nada que culpar.
Filename: "{sys}\sc.exe"; Parameters: "stop {#ServiceName}"; Flags: runhidden; RunOnceId: "PararServicio"
Filename: "{app}\{#DaemonExe}"; Parameters: "--uninstall-cleanup"; Flags: runhidden waituntilterminated; RunOnceId: "LimpiarTodo"
Filename: "{sys}\sc.exe"; Parameters: "delete {#ServiceName}"; Flags: runhidden; RunOnceId: "BorrarServicio"

[UninstallDelete]
; ProgramData no lo gestiona Inno porque no lo creo Inno.
Type: filesandordirs; Name: "{commonappdata}\{#AppName}"

[Code]
const
  ProfileListKey = 'SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList';

function Correr(const Exe, Params: string; var Codigo: Integer): Boolean;
begin
  Result := Exec(Exe, Params, '', SW_HIDE, ewWaitUntilTerminated, Codigo);
end;

procedure CorrerObligatorio(const Exe, Params, Accion: string);
var
  Codigo: Integer;
begin
  if not Correr(Exe, Params, Codigo) then
    RaiseException(Accion + ': no se pudo ejecutar ' + Exe + ': ' +
      SysErrorMessage(Codigo));
  if Codigo <> 0 then
    RaiseException(Accion + ': ' + Exe + ' termino con codigo ' +
      IntToStr(Codigo));
end;

function SysExe(const Nombre: string): string;
begin
  Result := ExpandConstant('{sys}\') + Nombre;
end;

function ServicioExiste(): Boolean;
var
  Codigo: Integer;
begin
  if not Correr(SysExe('sc.exe'), 'query {#ServiceName}', Codigo) then
    RaiseException('No se pudo consultar el servicio de Kanpachi: ' +
      SysErrorMessage(Codigo));

  if Codigo = 0 then
    Result := True
  else if Codigo = 1060 then
    Result := False
  else
    RaiseException('Consultar el servicio de Kanpachi termino con codigo ' +
      IntToStr(Codigo));
end;

function ContieneEstadoDetenido(const Lineas: TArrayOfString): Boolean;
var
  I: Integer;
begin
  Result := False;
  for I := 0 to GetArrayLength(Lineas) - 1 do
    if Pos(': 1 ', Lineas[I]) > 0 then
    begin
      Result := True;
      Exit;
    end;
end;

function ServicioEstaDetenido(): Boolean;
var
  Codigo: Integer;
  Salida: TExecOutput;
begin
  if not ExecAndCaptureOutput(SysExe('sc.exe'), 'query {#ServiceName}', '',
    SW_SHOWNORMAL, ewWaitUntilTerminated, Codigo, Salida) then
    RaiseException('No se pudo consultar el estado del servicio de Kanpachi: ' +
      SysErrorMessage(Codigo));

  if Codigo = 1060 then
  begin
    Result := True;
    Exit;
  end;
  if Codigo <> 0 then
    RaiseException('Consultar el estado del servicio de Kanpachi termino con codigo ' +
      IntToStr(Codigo));

  Result := ContieneEstadoDetenido(Salida.StdOut);
end;

function DetenerServicioInstalado(): Boolean;
var
  Codigo, Intento: Integer;
begin
  if not ServicioExiste() then
  begin
    Result := True;
    Exit;
  end;

  if ServicioEstaDetenido() then
  begin
    Result := True;
    Exit;
  end;

  { `sc stop` solo manda la orden. El daemon puede tardar mientras cierra una
    sala y restaura el firewall, asi que se espera el estado 1 (STOPPED) antes
    de reemplazar su ejecutable. No se mata ningun proceso portable. }
  if not Correr(SysExe('sc.exe'), 'stop {#ServiceName}', Codigo) then
    RaiseException('No se pudo pedir que se detuviera el servicio de Kanpachi: ' +
      SysErrorMessage(Codigo));
  if Codigo <> 0 then
    RaiseException('Detener el servicio de Kanpachi termino con codigo ' +
      IntToStr(Codigo));

  for Intento := 1 to 480 do
  begin
    if ServicioEstaDetenido() then
    begin
      Result := True;
      Exit;
    end;
    Sleep(250);
  end;
  Result := False;
end;

{ Avisa de lo unico que este instalador hace y no se puede deshacer.

  Actualizar detiene el servicio, y detenerlo CIERRA la sala que hubiera
  abierta: se cierran los puertos, se revierten las reglas del firewall y el
  motor cae con el Job. Es lo que tiene que pasar —no se reemplaza el binario
  de un daemon vivo— pero hasta ahora no lo decia nada en pantalla, y quien
  actualiza en mitad de una partida se entera cuando los demas se caen.

  Solo lo pregunta cuando el servicio esta CORRIENDO, o sea cuando hay algo
  que perder. Una instalacion nueva, o una sobre un Kanpachi ya detenido, no
  ve este dialogo: un clic de mas en el caso en que la advertencia no aplica
  es como una advertencia deja de leerse.

  El dialogo de Inno que cierra la interfaz —el de CloseApplications, que esta
  en "yes" por omision— no cubre esto: nombra ficheros en uso, no dice que la
  sala se va a cerrar, y el daemon ni siquiera aparece ahi porque a este lo
  detenemos nosotros antes. }
function ConfirmarQueSeCierraLaSala(): Boolean;
begin
  Result := True;
  if not ServicioExiste() then
    Exit;
  if ServicioEstaDetenido() then
    Exit;

  Result := MsgBox('Kanpachi esta corriendo y hay que detenerlo para reemplazar sus archivos.' + #13#10#13#10 +
    'Si tienes una sala abierta, se va a cerrar para todos: se cierran los puertos del juego y ' +
    'los demas pierden la conexion. Una partida en curso se corta.' + #13#10#13#10 +
    'Vuelve a abrir la sala cuando termine la instalacion.' + #13#10#13#10 +
    'Continuar?', mbConfirmation, MB_OKCANCEL) = IDOK;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  try
    if not ConfirmarQueSeCierraLaSala() then
    begin
      Result := 'Instalacion cancelada. No se reemplazo ningun archivo y la sala sigue abierta.';
      Exit;
    end;
    if not DetenerServicioInstalado() then
      Result := 'El servicio instalado de Kanpachi no termino de cerrarse en 120 segundos.' +
        ' No se reemplazo ningun archivo. Vuelve a intentarlo.';
  except
    Result := GetExceptionMessage;
  end;
end;

procedure RegistrarServicio();
var
  Bin: string;
begin
  Bin := ExpandConstant('{app}\{#DaemonExe}');

  { `create` NO actualiza un servicio existente. Se crea solo cuando falta y
    despues `config` impone SIEMPRE la ruta recien instalada, la cuenta y el
    arranque. Esto repara tambien servicios dejados por builds de desarrollo.
    Los espacios tras los "=" son sintaxis obligatoria de sc.exe. }
  if not ServicioExiste() then
    CorrerObligatorio(SysExe('sc.exe'),
      'create {#ServiceName} binPath= "' + Bin + '" type= own start= delayed-auto obj= LocalSystem DisplayName= "Kanpachi"',
      'Creando el servicio de Kanpachi');

  CorrerObligatorio(SysExe('sc.exe'),
    'config {#ServiceName} binPath= "' + Bin + '" type= own start= delayed-auto obj= LocalSystem DisplayName= "Kanpachi"',
    'Actualizando el servicio de Kanpachi');

  CorrerObligatorio(SysExe('sc.exe'),
    'description {#ServiceName} "Opens the ports your LAN games need while you play, only toward the people in your room, and closes them again when you leave."',
    'Escribiendo la descripcion del servicio');

  { Reintentar a los 5 s, 10 s y 30 s. Si el daemon se cae, la sala se cae con
    el: volver solo es la diferencia entre partida interrumpida y perdida. }
  CorrerObligatorio(SysExe('sc.exe'),
    'failure {#ServiceName} reset= 86400 actions= restart/5000/restart/10000/restart/30000',
    'Configurando la recuperacion del servicio');

  { La concesion minima, y lo que mantiene la promesa de un solo UAC: con el
    daemon parado, el acceso directo tiene que poder arrancar el servicio.

    Es el descriptor por defecto con UNA diferencia: la entrada de usuarios
    interactivos (IU) pasa de CCLCSWLOCRRC a CCLCSWRPWPLOCRRC, o sea gana RP
    (arrancar) y WP (detener). Lo demas se escribe igual porque sdset REEMPLAZA
    el descriptor entero. No se concede CCDC ni WD/WO: el usuario no puede
    reconfigurar ni borrar el servicio. }
  CorrerObligatorio(SysExe('sc.exe'),
    'sdset {#ServiceName} ' +
    'D:(A;;CCLCSWRPWPDTLOCRRC;;;SY)' +
    '(A;;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;BA)' +
    '(A;;CCLCSWRPWPLOCRRC;;;IU)' +
    '(A;;CCLCSWLOCRRC;;;SU)' +
    'S:(AU;FA;CCDCLCSWRPWPDTLOCRSDRCWDWO;;;WD)',
    'Concediendo el arranque del servicio al usuario');
end;

{ ProgramData\Kanpachi con su ACL propia.

  Esta ACL es la mitad de la proteccion del token de la API local, y por eso el
  daemon se NIEGA a crear el directorio: crearlo el heredaria la de ProgramData
  y la perderia en silencio.

  Escritura para SYSTEM y Administradores. LECTURA para los usuarios: la
  interfaz corre sin elevar y tiene que poder leer api.token. Lo que acota la
  superficie no es este permiso, es la lista cerrada de metodos del protocolo.

  /inheritance:r es la mitad que se olvida: sin ella la ACL nueva se SUMA a la
  heredada. }
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
begin
  if CurStep = ssPostInstall then
  begin
    CrearDirectorioDeDatos();
    RegistrarServicio();
    { El servicio NO se arranca aca: lo arranca el paso [Run], que ademas pide
      la ventana. Por las dos vias quedaria un daemon en silencio. }
  end;
end;

function InitializeUninstall(): Boolean;
begin
  Result := MsgBox('Se va a desinstalar Kanpachi.' + #13#10#13#10 +
    'Si hay una sala abierta se cerrara, y los puertos que Kanpachi tenia ' +
    'bloqueados para protegerte volveran a su estado anterior.' + #13#10#13#10 +
    'Continuar?', mbConfirmation, MB_YESNO) = IDYES;
end;

procedure BorrarPreferenciasDeLaUI();
var
  Perfiles: TArrayOfString;
  I: Integer;
  Perfil, DirectorioApp, DirectorioPublisher, Preferencias: string;
begin
  { Flutter no guarda estas preferencias junto al ejecutable. En Windows,
    shared_preferences usa path_provider.getApplicationSupportPath(), que
    deriva Roaming AppData\CompanyName\ProductName desde VERSIONINFO.

    No se usa la constante de AppData del usuario actual: este es un instalador
    por maquina y quien autoriza
    el UAC puede no ser quien ejecuto la UI. El desinstalador ya esta elevado,
    asi que recorre los perfiles registrados y borra SOLO el JSON de Kanpachi.
    Los directorios padre se quitan unicamente si quedaron vacios. }
  if not RegGetSubkeyNames(HKLM64, ProfileListKey, Perfiles) then
  begin
    Log('No se pudo enumerar ProfileList; no se borraron las preferencias de la UI.');
    Exit;
  end;

  for I := 0 to GetArrayLength(Perfiles) - 1 do
  begin
    if not RegQueryStringValue(HKLM64,
      ProfileListKey + '\' + Perfiles[I], 'ProfileImagePath', Perfil) then
    begin
      Log('Un perfil registrado no tiene ProfileImagePath; se omite.');
      Continue;
    end;

    { Los perfiles normales traen una ruta absoluta. Los perfiles de servicio
      suelen usar estas dos variables; expandirlas evita construir una ruta
      literal con signos de porcentaje. }
    StringChange(Perfil, '%SystemDrive%', ExpandConstant('{sd}'));
    StringChange(Perfil, '%SystemRoot%', ExpandConstant('{win}'));
    if Pos('%', Perfil) > 0 then
    begin
      Log('ProfileImagePath contiene una variable desconocida; se omite por seguridad.');
      Continue;
    end;

    DirectorioPublisher := AddBackslash(Perfil) +
      'AppData\Roaming\{#AppPublisher}';
    DirectorioApp := AddBackslash(DirectorioPublisher) + '{#AppName}';
    Preferencias := AddBackslash(DirectorioApp) + 'shared_preferences.json';

    if FileExists(Preferencias) then
    begin
      if DeleteFile(Preferencias) then
        Log('Preferencias de la UI eliminadas de un perfil.')
      else
        Log('No se pudieron eliminar las preferencias de la UI de un perfil.');
    end;
    RemoveDir(DirectorioApp);
    RemoveDir(DirectorioPublisher);
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    BorrarPreferenciasDeLaUI();
end;
