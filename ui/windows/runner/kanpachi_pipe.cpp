#include "kanpachi_pipe.h"

#include <flutter/event_channel.h>
#include <flutter/event_stream_handler_functions.h>
#include <flutter/method_channel.h>
#include <flutter/standard_method_codec.h>

#include <windows.h>

#include <cstdint>
#include <deque>
#include <functional>
#include <map>
#include <memory>
#include <mutex>
#include <string>
#include <thread>
#include <utility>
#include <vector>

namespace {

using EV = flutter::EncodableValue;
using EMap = flutter::EncodableMap;

// Compartido y no `unique_ptr` porque el resultado cruza al hilo de plataforma
// dentro de un `std::function`, y `std::function` exige que lo que captura se
// pueda copiar.
using Result = std::shared_ptr<flutter::MethodResult<EV>>;

constexpr const char kMethodChannel[] = "kanpachi/pipe";
constexpr const char kEventChannel[] = "kanpachi/pipe/events";
constexpr wchar_t kQueueClassName[] = L"KanpachiPipeQueue";
constexpr UINT kMsgRunTasks = WM_APP + 0x51;

// Igual que el buffer que el daemon le pide a go-winio, así una lectura entera
// nunca necesita dos viajes.
constexpr DWORD kReadBytes = 64 * 1024;

// Por qué se acabó el canal. **Los tres motivos que dan cero se separan acá**,
// y esa mezcla ya costó una investigación: `error de Windows 0` significaba a la
// vez fin de archivo, parada pedida y espera fallida, o sea nada.
constexpr const char kWhyEof[] = "eof";          // cero bytes: el otro se fue
constexpr const char kWhyStopped[] = "stopped";  // lo pedimos nosotros
constexpr const char kWhyIo[] = "io";            // error de lectura
constexpr const char kWhyWait[] = "wait";        // la espera misma falló

// ---------------------------------------------------------------------------
// Cola hacia el hilo de plataforma
// ---------------------------------------------------------------------------

// Los canales de Flutter SOLO se pueden tocar desde el hilo de plataforma, y
// los hilos de las conexiones no son ese. El embebedor de Windows no expone un
// task runner al C++ del runner, así que el vehículo es una ventana
// message-only propia: `PostMessage` es seguro entre hilos, y el bucle de
// mensajes de `wWinMain` la despacha en el hilo correcto.
//
// Es una ventana PROPIA a propósito, en vez de engancharse al window proc de la
// aplicación: lo segundo mete código nuestro en el camino de todos los mensajes
// de la ventana real, y una de las firmas de caída que se está persiguiendo es
// justamente una excepción dentro de un window proc.
class PlatformQueue {
 public:
  bool Start() {
    static bool registered = false;
    if (!registered) {
      WNDCLASSEXW wc = {};
      wc.cbSize = sizeof(wc);
      wc.lpfnWndProc = PlatformQueue::WndProc;
      wc.hInstance = GetModuleHandleW(nullptr);
      wc.lpszClassName = kQueueClassName;
      if (RegisterClassExW(&wc) == 0) {
        return false;
      }
      registered = true;
    }
    hwnd_ = CreateWindowExW(0, kQueueClassName, nullptr, 0, 0, 0, 0, 0,
                            HWND_MESSAGE, nullptr, GetModuleHandleW(nullptr),
                            this);
    return hwnd_ != nullptr;
  }

  void Stop() {
    if (hwnd_ != nullptr) {
      DestroyWindow(hwnd_);
      hwnd_ = nullptr;
    }
    std::lock_guard<std::mutex> lock(mutex_);
    tasks_.clear();
  }

  // Desde CUALQUIER hilo.
  void Post(std::function<void()> task) {
    HWND target = nullptr;
    {
      std::lock_guard<std::mutex> lock(mutex_);
      if (hwnd_ == nullptr) {
        return;
      }
      tasks_.push_back(std::move(task));
      target = hwnd_;
    }
    PostMessageW(target, kMsgRunTasks, 0, 0);
  }

 private:
  static LRESULT CALLBACK WndProc(HWND hwnd, UINT message, WPARAM wparam,
                                  LPARAM lparam) {
    if (message == WM_NCCREATE) {
      auto* create = reinterpret_cast<CREATESTRUCTW*>(lparam);
      SetWindowLongPtrW(hwnd, GWLP_USERDATA,
                        reinterpret_cast<LONG_PTR>(create->lpCreateParams));
    } else if (message == kMsgRunTasks) {
      auto* self = reinterpret_cast<PlatformQueue*>(
          GetWindowLongPtrW(hwnd, GWLP_USERDATA));
      if (self != nullptr) {
        self->Drain();
      }
      return 0;
    }
    return DefWindowProcW(hwnd, message, wparam, lparam);
  }

  // Se vacía la cola entera por mensaje: `PostMessage` puede fusionar avisos si
  // la cola de Windows se llena, y una tarea que se quedara esperando otro
  // aviso que ya no llega es una petición que nunca contesta.
  void Drain() {
    for (;;) {
      std::function<void()> task;
      {
        std::lock_guard<std::mutex> lock(mutex_);
        if (tasks_.empty()) {
          return;
        }
        task = std::move(tasks_.front());
        tasks_.pop_front();
      }
      task();
    }
  }

  HWND hwnd_ = nullptr;
  std::mutex mutex_;
  std::deque<std::function<void()>> tasks_;
};

class PipeHost;

// ---------------------------------------------------------------------------
// Una conexión
// ---------------------------------------------------------------------------

class PipeConnection {
 public:
  PipeConnection(int id, std::wstring name, int busy_retries, PipeHost* host)
      : id_(id),
        name_(std::move(name)),
        busy_retries_(busy_retries),
        host_(host) {}

  ~PipeConnection() {
    Stop();
    if (pipe_ != INVALID_HANDLE_VALUE) {
      CloseHandle(pipe_);
    }
    if (stop_ != nullptr) {
      CloseHandle(stop_);
    }
    if (wake_ != nullptr) {
      CloseHandle(wake_);
    }
  }

  PipeConnection(const PipeConnection&) = delete;
  PipeConnection& operator=(const PipeConnection&) = delete;

  // Arranca el hilo dueño del handle. `opened` se contesta desde el hilo de
  // plataforma, pase lo que pase.
  bool Start(Result opened);

  // Encola bytes. El resultado se contesta cuando salieron de verdad.
  void Enqueue(std::vector<uint8_t> bytes, Result written);

  // Señala la parada y JUNTA el hilo. Idempotente. Hilo de plataforma.
  void Stop();

 private:
  struct Job {
    std::vector<uint8_t> bytes;
    Result result;
  };

  void ThreadMain(Result opened);
  bool Open(DWORD* error);
  void Loop();
  // Devuelve false cuando hay que parar.
  bool DrainWrites(HANDLE write_event, OVERLAPPED* wo);
  void FinishWrite(const Result& result, DWORD error);
  // Falla lo que quedó en la cola. Después de esto `Enqueue` rebota solo.
  void FailPending();

  const int id_;
  const std::wstring name_;
  const int busy_retries_;
  PipeHost* const host_;

  HANDLE pipe_ = INVALID_HANDLE_VALUE;
  HANDLE stop_ = nullptr;
  HANDLE wake_ = nullptr;
  std::thread worker_;
  bool stopping_ = false;

  std::mutex queue_mutex_;
  std::deque<Job> queue_;
  bool closed_ = false;
};

// ---------------------------------------------------------------------------
// El anfitrión: canales, mapa de conexiones y cola
// ---------------------------------------------------------------------------

// Todo lo de esta clase corre en el hilo de plataforma salvo `EmitData` y
// `EmitClosed`, que son lo que los hilos de las conexiones llaman y lo único
// que pasa por la cola. Por eso el mapa no lleva mutex: no hay dos hilos.
class PipeHost {
 public:
  // Se cuelga del mensajero del motor y NO de un `PluginRegistrar`: ese vive en
  // `flutter_wrapper_plugin`, que el runner no enlaza, y traerlo solo por el
  // constructor duplicaría en el binario lo que `flutter_wrapper_app` ya trae.
  explicit PipeHost(flutter::FlutterEngine* engine) {
    methods_ = std::make_unique<flutter::MethodChannel<EV>>(
        engine->messenger(), kMethodChannel,
        &flutter::StandardMethodCodec::GetInstance());
    methods_->SetMethodCallHandler(
        [this](const flutter::MethodCall<EV>& call,
               std::unique_ptr<flutter::MethodResult<EV>> result) {
          HandleCall(call, Result(std::move(result)));
        });

    events_ = std::make_unique<flutter::EventChannel<EV>>(
        engine->messenger(), kEventChannel,
        &flutter::StandardMethodCodec::GetInstance());
    events_->SetStreamHandler(
        std::make_unique<flutter::StreamHandlerFunctions<EV>>(
            [this](const EV*, std::unique_ptr<flutter::EventSink<EV>>&& sink)
                -> std::unique_ptr<flutter::StreamHandlerError<EV>> {
              sink_ = std::move(sink);
              for (EMap& held : pending_) {
                sink_->Success(EV(std::move(held)));
              }
              pending_.clear();
              return nullptr;
            },
            [this](const EV*)
                -> std::unique_ptr<flutter::StreamHandlerError<EV>> {
              sink_ = nullptr;
              return nullptr;
            }));

    started_ = queue_.Start();
  }

  ~PipeHost() {
    // Las conexiones primero: sus hilos pueden estar posteando a la cola.
    conns_.clear();
    queue_.Stop();
    sink_ = nullptr;
  }

  PipeHost(const PipeHost&) = delete;
  PipeHost& operator=(const PipeHost&) = delete;

  bool started() const { return started_; }
  PlatformQueue& queue() { return queue_; }

  // Desde el hilo de una conexión.
  void EmitData(int id, const uint8_t* data, size_t count) {
    std::vector<uint8_t> copy(data, data + count);
    queue_.Post([this, id, copy = std::move(copy)]() mutable {
      EMap event;
      event[EV("id")] = EV(id);
      event[EV("kind")] = EV("data");
      event[EV("bytes")] = EV(std::move(copy));
      Emit(std::move(event));
    });
  }

  // Desde el hilo de una conexión, como último acto.
  void EmitClosed(int id, const char* why, DWORD error) {
    std::string reason(why);
    queue_.Post([this, id, reason = std::move(reason), error]() {
      EMap event;
      event[EV("id")] = EV(id);
      event[EV("kind")] = EV("closed");
      event[EV("why")] = EV(reason);
      event[EV("error")] = EV(static_cast<int32_t>(error));
      Emit(std::move(event));
      // Se olvida DESPUÉS de avisar, y en la misma tarea, para que el orden con
      // el aviso no dependa de dos mensajes distintos.
      Forget(id);
    });
  }

  void Forget(int id) { conns_.erase(id); }

 private:
  void Emit(EMap event) {
    if (sink_ == nullptr) {
      // Dart todavía no se suscribió. Pasa de verdad: `listen` del EventChannel
      // y `open` del MethodChannel son dos mensajes independientes, así que sin
      // esto los primeros bytes de una conexión rápida se perderían.
      pending_.push_back(std::move(event));
      return;
    }
    sink_->Success(EV(std::move(event)));
  }

  static bool ReadInt(const EMap& args, const char* key, int64_t* out) {
    const auto it = args.find(EV(key));
    if (it == args.end()) {
      return false;
    }
    // Nada de llamarle `small` a esto: `rpcndr.h`, que entra con `windows.h`,
    // define `small` como `char`.
    if (const auto* as32 = std::get_if<int32_t>(&it->second)) {
      *out = *as32;
      return true;
    }
    if (const auto* as64 = std::get_if<int64_t>(&it->second)) {
      *out = *as64;
      return true;
    }
    return false;
  }

  static const std::string* ReadString(const EMap& args, const char* key) {
    const auto it = args.find(EV(key));
    if (it == args.end()) {
      return nullptr;
    }
    return std::get_if<std::string>(&it->second);
  }

  static const std::vector<uint8_t>* ReadBytes(const EMap& args,
                                               const char* key) {
    const auto it = args.find(EV(key));
    if (it == args.end()) {
      return nullptr;
    }
    return std::get_if<std::vector<uint8_t>>(&it->second);
  }

  void HandleCall(const flutter::MethodCall<EV>& call, Result result) {
    const auto* args = std::get_if<EMap>(call.arguments());
    if (args == nullptr) {
      result->Error("args", "faltan los argumentos");
      return;
    }
    int64_t id = 0;
    if (!ReadInt(*args, "id", &id)) {
      result->Error("args", "falta el id de la conexión");
      return;
    }
    const int key = static_cast<int>(id);

    if (call.method_name() == "open") {
      Open(*args, key, std::move(result));
      return;
    }
    if (call.method_name() == "send") {
      Send(*args, key, std::move(result));
      return;
    }
    if (call.method_name() == "close") {
      Close(key, std::move(result));
      return;
    }
    result->NotImplemented();
  }

  void Open(const EMap& args, int key, Result result) {
    if (!started_) {
      result->Error("host", "el canal nativo no arrancó");
      return;
    }
    if (conns_.count(key) != 0) {
      result->Error("id", "ese id de conexión ya está en uso");
      return;
    }
    const std::string* name = ReadString(args, "name");
    if (name == nullptr) {
      result->Error("args", "falta el nombre del pipe");
      return;
    }
    int64_t retries = 0;
    if (!ReadInt(args, "busyRetries", &retries)) {
      retries = 3;
    }

    const int wide = MultiByteToWideChar(CP_UTF8, 0, name->c_str(),
                                         static_cast<int>(name->size()),
                                         nullptr, 0);
    std::wstring path(static_cast<size_t>(wide), L'\0');
    if (wide > 0) {
      MultiByteToWideChar(CP_UTF8, 0, name->c_str(),
                          static_cast<int>(name->size()), path.data(), wide);
    }

    auto conn = std::make_unique<PipeConnection>(
        key, std::move(path), static_cast<int>(retries), this);
    PipeConnection* raw = conn.get();
    conns_[key] = std::move(conn);
    if (!raw->Start(std::move(result))) {
      conns_.erase(key);
    }
  }

  void Send(const EMap& args, int key, Result result) {
    const auto it = conns_.find(key);
    if (it == conns_.end()) {
      result->Error("gone", "esa conexión ya no existe");
      return;
    }
    const std::vector<uint8_t>* bytes = ReadBytes(args, "bytes");
    if (bytes == nullptr) {
      result->Error("args", "faltan los bytes");
      return;
    }
    it->second->Enqueue(*bytes, std::move(result));
  }

  void Close(int key, Result result) {
    // Idempotente por contrato con `DaemonTransport.close`, que lo llama desde
    // el camino de error y puede correr antes de que se haya abierto nada.
    conns_.erase(key);
    result->Success();
  }

  std::unique_ptr<flutter::MethodChannel<EV>> methods_;
  std::unique_ptr<flutter::EventChannel<EV>> events_;
  std::unique_ptr<flutter::EventSink<EV>> sink_;
  std::deque<EMap> pending_;
  // **La cola se declara ANTES que las conexiones**, o sea que se destruye
  // después: los hilos de las conexiones postean acá hasta su último acto, y el
  // orden inverso dejaría a un hilo escribiendo en una cola ya destruida.
  PlatformQueue queue_;
  std::map<int, std::unique_ptr<PipeConnection>> conns_;
  bool started_ = false;
};

// ---------------------------------------------------------------------------

bool PipeConnection::Start(Result opened) {
  stop_ = CreateEventW(nullptr, TRUE, FALSE, nullptr);
  wake_ = CreateEventW(nullptr, FALSE, FALSE, nullptr);
  if (stop_ == nullptr || wake_ == nullptr) {
    const DWORD error = GetLastError();
    opened->Error("event", "no se pudieron crear los eventos de la conexión",
                  EV(static_cast<int32_t>(error)));
    return false;
  }
  worker_ = std::thread([this, opened]() { ThreadMain(opened); });
  return true;
}

void PipeConnection::Stop() {
  if (stopping_) {
    return;
  }
  stopping_ = true;
  if (stop_ != nullptr) {
    SetEvent(stop_);
  }
  if (worker_.joinable()) {
    // Se JUNTA, no se abandona. Es la diferencia entera con la versión de Dart:
    // mientras este hilo viva, el kernel puede estar escribiendo en su pila, y
    // cerrar el handle o soltar la memoria antes es la corrupción que se está
    // arreglando. Las dos esperas del bucle incluyen `stop_`, así que esto
    // vuelve en cuanto Windows despacha el evento.
    worker_.join();
  }
}

void PipeConnection::ThreadMain(Result opened) {
  DWORD error = 0;
  if (!Open(&error)) {
    // Por valor y no por `this`: la tarea DESTRUYE esta conexión al olvidarla,
    // así que leer un campo después de eso sería leer un objeto muerto.
    PipeHost* const host = host_;
    const int id = id_;
    host->queue().Post([host, id, opened, error]() {
      opened->Error("open", "no se pudo abrir el canal del daemon",
                    EV(static_cast<int32_t>(error)));
      host->Forget(id);
    });
    return;
  }
  host_->queue().Post([opened]() { opened->Success(); });
  Loop();
}

bool PipeConnection::Open(DWORD* error) {
  for (int attempt = 0;; ++attempt) {
    // `CreateFileW` sobre un named pipe no se queda esperando: conecta, o
    // vuelve en el acto diciendo ocupado o que no está.
    const HANDLE handle =
        CreateFileW(name_.c_str(), GENERIC_READ | GENERIC_WRITE, 0, nullptr,
                    OPEN_EXISTING, FILE_FLAG_OVERLAPPED, nullptr);
    if (handle != INVALID_HANDLE_VALUE) {
      pipe_ = handle;
      return true;
    }
    const DWORD failure = GetLastError();
    if (failure == ERROR_PIPE_BUSY && attempt < busy_retries_) {
      // La espera cuelga de `stop_` en vez de ser un `Sleep`, así que cerrar
      // durante los reintentos no tarda medio segundo en hacerse efectivo.
      if (WaitForSingleObject(stop_, 120) == WAIT_OBJECT_0) {
        *error = ERROR_OPERATION_ABORTED;
        return false;
      }
      continue;
    }
    *error = failure;
    return false;
  }
}

void PipeConnection::Loop() {
  const HANDLE read_event = CreateEventW(nullptr, TRUE, FALSE, nullptr);
  const HANDLE write_event = CreateEventW(nullptr, TRUE, FALSE, nullptr);
  if (read_event == nullptr || write_event == nullptr) {
    const DWORD error = GetLastError();
    if (read_event != nullptr) {
      CloseHandle(read_event);
    }
    if (write_event != nullptr) {
      CloseHandle(write_event);
    }
    FailPending();
    host_->EmitClosed(id_, kWhyIo, error);
    return;
  }

  OVERLAPPED ro = {};
  OVERLAPPED wo = {};
  // Vive tanto como el hilo, o sea tanto como cualquier lectura que lo apunte.
  std::vector<uint8_t> buffer(kReadBytes);
  bool read_pending = false;
  const char* why = kWhyStopped;
  DWORD error = 0;

  for (;;) {
    if (!read_pending) {
      ZeroMemory(&ro, sizeof(ro));
      ro.hEvent = read_event;
      ResetEvent(read_event);
      if (!ReadFile(pipe_, buffer.data(), kReadBytes, nullptr, &ro)) {
        const DWORD failure = GetLastError();
        if (failure != ERROR_IO_PENDING) {
          why = kWhyIo;
          error = failure;
          break;
        }
      }
      read_pending = true;
    }

    const HANDLE waits[3] = {read_event, wake_, stop_};
    const DWORD signalled = WaitForMultipleObjects(3, waits, FALSE, INFINITE);

    if (signalled == WAIT_OBJECT_0) {
      DWORD moved = 0;
      if (!GetOverlappedResult(pipe_, &ro, &moved, FALSE)) {
        const DWORD failure = GetLastError();
        // No es un fallo: el kernel todavía es dueño de `ro`. Volver a esperar
        // es lo único correcto, porque salir dejaría una lectura viva apuntando
        // a memoria que está por desaparecer.
        if (failure == ERROR_IO_INCOMPLETE) {
          continue;
        }
        read_pending = false;
        why = kWhyIo;
        error = failure;
        break;
      }
      read_pending = false;
      if (moved == 0) {
        why = kWhyEof;
        break;
      }
      host_->EmitData(id_, buffer.data(), moved);
      continue;
    }

    if (signalled == WAIT_OBJECT_0 + 1) {
      if (!DrainWrites(write_event, &wo)) {
        break;
      }
      continue;
    }

    if (signalled == WAIT_OBJECT_0 + 2) {
      break;
    }

    why = kWhyWait;
    error = GetLastError();
    break;
  }

  // **La lectura pendiente se cobra antes de que nada se destruya.** `buffer` y
  // `ro` viven en este hilo, y el kernel escribe en los dos cuando la operación
  // termina. Cancelar y esperar DE VERDAD al resultado es lo único que garantiza
  // que nadie más los toque.
  if (read_pending) {
    CancelIoEx(pipe_, &ro);
    DWORD moved = 0;
    GetOverlappedResult(pipe_, &ro, &moved, TRUE);
  }
  CloseHandle(read_event);
  CloseHandle(write_event);

  FailPending();
  host_->EmitClosed(id_, why, error);
}

bool PipeConnection::DrainWrites(HANDLE write_event, OVERLAPPED* wo) {
  for (;;) {
    Job job;
    {
      std::lock_guard<std::mutex> lock(queue_mutex_);
      if (queue_.empty()) {
        return true;
      }
      job = std::move(queue_.front());
      queue_.pop_front();
    }

    ZeroMemory(wo, sizeof(*wo));
    wo->hEvent = write_event;
    ResetEvent(write_event);

    if (!WriteFile(pipe_, job.bytes.data(),
                   static_cast<DWORD>(job.bytes.size()), nullptr, wo)) {
      const DWORD failure = GetLastError();
      if (failure != ERROR_IO_PENDING) {
        FinishWrite(job.result, failure);
        continue;
      }
    }

    const HANDLE waits[2] = {write_event, stop_};
    const DWORD signalled = WaitForMultipleObjects(2, waits, FALSE, INFINITE);
    if (signalled != WAIT_OBJECT_0) {
      CancelIoEx(pipe_, wo);
      DWORD moved = 0;
      GetOverlappedResult(pipe_, wo, &moved, TRUE);
      FinishWrite(job.result, ERROR_OPERATION_ABORTED);
      return false;
    }

    DWORD moved = 0;
    if (!GetOverlappedResult(pipe_, wo, &moved, FALSE)) {
      const DWORD failure = GetLastError();
      if (failure == ERROR_IO_INCOMPLETE) {
        CancelIoEx(pipe_, wo);
        GetOverlappedResult(pipe_, wo, &moved, TRUE);
      }
      // Falla ESTA petición y sigue, en vez de matar el canal. Quien decide que
      // el enlace se acabó es el lector, que es el único que distingue "el otro
      // extremo se fue" de "esta escritura salió mal", y así el motivo que
      // llega a Dart no se inventa desde acá.
      FinishWrite(job.result, failure);
      continue;
    }
    FinishWrite(job.result, 0);
    // `job` —y con él el buffer que el kernel acaba de leer— muere recién acá,
    // después de que la operación está cobrada.
  }
}

void PipeConnection::FinishWrite(const Result& result, DWORD error) {
  host_->queue().Post([result, error]() {
    if (error == 0) {
      result->Success();
      return;
    }
    result->Error("write", "la escritura no salió",
                  EV(static_cast<int32_t>(error)));
  });
}

void PipeConnection::FailPending() {
  std::deque<Job> left;
  {
    std::lock_guard<std::mutex> lock(queue_mutex_);
    closed_ = true;
    left.swap(queue_);
  }
  for (Job& job : left) {
    FinishWrite(job.result, ERROR_OPERATION_ABORTED);
  }
}

void PipeConnection::Enqueue(std::vector<uint8_t> bytes, Result written) {
  {
    std::lock_guard<std::mutex> lock(queue_mutex_);
    if (!closed_) {
      queue_.push_back(Job{std::move(bytes), std::move(written)});
      SetEvent(wake_);
      return;
    }
  }
  // El hilo ya se fue: se contesta acá mismo, que es el hilo de plataforma. Sin
  // esto la petición se quedaría esperando para siempre.
  written->Error("gone", "el canal ya estaba cerrado",
                 EV(static_cast<int32_t>(ERROR_OPERATION_ABORTED)));
}

std::unique_ptr<PipeHost> g_host;

}  // namespace

void RegisterKanpachiPipe(flutter::FlutterEngine* engine) {
  if (engine == nullptr || g_host != nullptr) {
    return;
  }
  g_host = std::make_unique<PipeHost>(engine);
}

void UnregisterKanpachiPipe() { g_host = nullptr; }
