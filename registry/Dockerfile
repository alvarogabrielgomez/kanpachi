# kanpachi-registry: el registro de salas del seed.
#
# Se construye desde la raíz del repositorio, porque necesita el módulo Go
# entero y la página:
#
#   docker build -f registry/Dockerfile -t kanpachi-registry:dev .
#
# La imagen final SE APOYA en la de EasyTier, y eso es deliberado. El registro
# lee el contador de miembros invocando easytier-cli, así que necesita ese
# binario y necesita que corra: copiarlo a una base cualquiera es apostar a que
# coincidan libc y versión. Partir de su propia imagen elimina la apuesta.
#
# Es agregación, no vinculación. easytier-core y easytier-cli son ejecutables
# LGPL-3.0 que conviven con el nuestro y se invocan como procesos; nada de
# Kanpachi se enlaza contra ellos. Al distribuir esta imagen viaja también su
# licencia, igual que en cualquier distribución que empaqueta ambos.

FROM golang:1.25-alpine AS build
WORKDIR /src

# Las dependencias primero, para que editar el código no invalide su capa.
COPY go.mod go.sum ./
RUN go mod download

COPY core/ core/
COPY registry/ registry/

# Sin -ldflags "-s -w": los símbolos pesan poco y quitarlos dispara falsos
# positivos de antivirus sobre binarios Go, que es una regla del proyecto.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/kanpachi-registry ./registry/cmd/kanpachi-registry

FROM easytier/easytier:v2.6.4

COPY --from=build /out/kanpachi-registry /usr/local/bin/kanpachi-registry
COPY invite/index.html /srv/index.html

# Dentro del contenedor escucha en 0.0.0.0, y quien restringe es el compose,
# que lo publica como 127.0.0.1:8010 en el host. Tiene que ser así: publicar un
# puerto usa DNAT hacia la IP del contenedor, no su loopback, o sea que un bind
# a 127.0.0.1 aquí adentro jamás recibiría nada.
#
# Que quede solo en el loopback del host no es comodidad: el límite de tasa lee
# X-Forwarded-For, y exponer esto directo a internet permitiría falsificar esa
# cabecera y anularlo entero. El proxy inverso es parte de la defensa.
EXPOSE 8010

ENTRYPOINT ["/usr/local/bin/kanpachi-registry"]
CMD ["--addr", "0.0.0.0:8010", "--page", "/srv/index.html", "--easytier-cli", "easytier-cli", "--rpc-portal", "127.0.0.1:15888"]
