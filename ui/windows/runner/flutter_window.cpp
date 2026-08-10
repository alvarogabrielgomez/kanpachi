#include "flutter_window.h"

#include <optional>

#include "flutter/generated_plugin_registrant.h"
#include "kanpachi_pipe.h"

FlutterWindow::FlutterWindow(const flutter::DartProject& project)
    : project_(project) {}

FlutterWindow::~FlutterWindow() {}

bool FlutterWindow::OnCreate() {
  if (!Win32Window::OnCreate()) {
    return false;
  }

  RECT frame = GetClientArea();

  // The size here must match the window dimensions to avoid unnecessary surface
  // creation / destruction in the startup path.
  flutter_controller_ = std::make_unique<flutter::FlutterViewController>(
      frame.right - frame.left, frame.bottom - frame.top, project_);
  // Ensure that basic setup of the controller was successful.
  if (!flutter_controller_->engine() || !flutter_controller_->view()) {
    return false;
  }
  RegisterPlugins(flutter_controller_->engine());
  // El named pipe del daemon. No es un plugin de pub a proposito: ver la nota
  // de kanpachi_pipe.h.
  RegisterKanpachiPipe(flutter_controller_->engine());
  SetChildContent(flutter_controller_->view()->GetNativeWindow());

  // La plantilla de Flutter ensena la ventana en el primer fotograma, desde
  // aca. Se quito a proposito, y quien decide ensenarla es Dart.
  //
  // # Por que
  //
  // Porque Kanpachi arranca en silencio cuando lo levanta Windows al encender
  // la PC: aparece el icono de la bandeja y no la ventana. Con el
  // SetNextFrameCallback puesto, ese modo era imposible desde Dart: la ventana
  // se ensenaba desde C++ en cuanto habia un fotograma, pasara lo que pasara
  // arriba.
  //
  // MEDIDO, y solo se ve ejecutandolo: con --silent y sin llamar a show(),
  // IsWindowVisible seguia contestando true. Ningun test de Dart lo puede ver,
  // porque lo decide C++.
  //
  // La ventana nace oculta sola: CreateWindow usa WS_OVERLAPPEDWINDOW, que no
  // trae WS_VISIBLE. Quien la ensena ahora es windowManager.show() en
  // lib/main.dart, y solo cuando no se pidio silencio.
  //
  // El ForceRedraw se queda: hace falta igual para que haya un fotograma listo
  // cuando Dart pida ensenarla, y sin el la ventana aparecia en blanco.
  flutter_controller_->ForceRedraw();

  return true;
}

void FlutterWindow::OnDestroy() {
  // ANTES de soltar el motor: junta los hilos del pipe y suelta los canales.
  // Un resultado contestado despues de que el motor se fue escribe sobre un
  // mensajero que ya no existe.
  UnregisterKanpachiPipe();

  if (flutter_controller_) {
    flutter_controller_ = nullptr;
  }

  Win32Window::OnDestroy();
}

LRESULT
FlutterWindow::MessageHandler(HWND hwnd, UINT const message,
                              WPARAM const wparam,
                              LPARAM const lparam) noexcept {
  // Give Flutter, including plugins, an opportunity to handle window messages.
  if (flutter_controller_) {
    std::optional<LRESULT> result =
        flutter_controller_->HandleTopLevelWindowProc(hwnd, message, wparam,
                                                      lparam);
    if (result) {
      return *result;
    }
  }

  switch (message) {
    case WM_FONTCHANGE:
      flutter_controller_->engine()->ReloadSystemFonts();
      break;
  }

  return Win32Window::MessageHandler(hwnd, message, wparam, lparam);
}
