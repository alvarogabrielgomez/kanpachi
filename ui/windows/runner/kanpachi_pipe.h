#ifndef RUNNER_KANPACHI_PIPE_H_
#define RUNNER_KANPACHI_PIPE_H_

#include <flutter/flutter_engine.h>

// El named pipe del daemon, hablado desde C++ y expuesto a Dart por canales.
//
// # Por qué acá y no en `dart:ffi`
//
// Porque la versión en Dart no tenía dónde poner el dueño de la memoria. Una
// `ReadFile` superpuesta le regala al kernel dos punteros —el `OVERLAPPED` y el
// buffer— hasta que la operación termina DE VERDAD, y en Dart esos punteros
// eran `calloc` sueltos cuya vida dependía de que un isolate llegara a su
// `finally`. Un isolate no garantiza ni hilo propio ni que ese `finally` corra:
// `Isolate.kill` no interrumpe una llamada nativa, y el dueño cerraba el handle
// por debajo tras una gracia de tres segundos.
//
// Medido el 2026-08-09 sobre 32 horas: **49 caídas de `kanpachiui.exe`**, nueve
// de ellas `0xC0000374`, o sea `STATUS_HEAP_CORRUPTION` — el gestor de heap
// cazando una escritura fuera de sitio. Las otras aparecían en `ntdll` y en
// `flutter_windows`, que es lo que hace la memoria corrupta: mata al siguiente
// que toca la zona, no al que la rompió.
//
// Acá el buffer de cada escritura es un `std::vector` local que vive hasta
// después de cobrar la operación, la lectura pendiente se cancela y se espera
// antes de que nada se destruya, y el handle lo cierra el mismo hilo que lo
// usó. Nadie comparte punteros con nadie.
//
// # Por qué en el runner y no en un paquete de pub
//
// Es la puerta de un daemon que corre elevado. Vale la misma razón por la que
// `win32` está clavado sin acento circunflejo en `pubspec.yaml`: subir esa
// superficie tiene que ser un commit a propósito, no una resolución de
// dependencias.
void RegisterKanpachiPipe(flutter::FlutterEngine* engine);

// Cierra todas las conexiones y suelta los canales.
//
// Se llama ANTES de destruir el motor: un `MethodResult` completado después de
// que el motor se fue escribe sobre un mensajero que ya no existe, que es
// justamente la familia de fallos de la que este archivo viene huyendo.
void UnregisterKanpachiPipe();

#endif  // RUNNER_KANPACHI_PIPE_H_
