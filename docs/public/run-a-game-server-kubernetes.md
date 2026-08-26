# Run a game server on Kubernetes

Kanpachi runs beside your game server as a **native sidecar**, in the same pod
and the same network namespace. The game keeps listening where it always did,
Kanpachi opens the room, and you paste the code into the group chat once.

If the machine is not running Kubernetes, [the Docker guide](run-a-game-server-docker.md)
is shorter and the result is the same room.

## Before anything

Read [the Docker guide](run-a-game-server-docker.md) first. Everything it says
about templates, ports, the password and picking a version applies here
unchanged. This page covers only what Kubernetes does differently.

Three of those differences will cost you a whole evening if you meet them by
surprise, so they are the first three sections.

## Keep the CNI out of `100.64.0.0/10`

Kanpachi hands every room a `/24` out of `100.64.0.0/10`. Several CNIs hand out
pod and service addresses from the same block.

If they overlap, Kanpachi refuses to reopen the room, permanently: joining its
own room would take away the network it arrived on. There is no flag to force
it and no way to recover the room afterwards.

Check before you deploy:

```sh
kubectl cluster-info dump | grep -m2 -E 'cluster-cidr|service-cluster-ip-range'
```

Anything inside `100.64.0.0/10` has to move before Kanpachi runs. Kanpachi also
uses `10.99.0.0/16` for rooms and `198.19.0.0/16` for lobbies.

## Expect relay, not direct

A pod without `hostNetwork: true` leaves through the node's network with address
translation in the way, and from outside that reads as symmetric NAT. Kanpachi
detects it and routes the room through a relay. The room works. It is slower
than a direct path, and no amount of configuring inside the pod changes it.

For a direct path, set `hostNetwork: true` and accept what that brings: the pod
shares the node's network namespace, so its ports are the node's ports.

## Do not override `command`

The image's entrypoint sets `KANPACHI_CONTAINER=1`. That flag is what lets
Kanpachi send the room's traffic to wherever the game actually listens, which is
the whole reason the sidecar works without touching the game's configuration.

A manifest that sets its own `command` loses the flag. Nothing fails: the room
opens, the code works, people join, and the game never answers.

## The sidecar

Kanpachi has to be up before the game and stay up for the pod's life, which is
what a **native sidecar** is: an init container with `restartPolicy: Always`.

```yaml
spec:
  template:
    spec:
      initContainers:
        - name: kanpachi
          image: ghcr.io/alvarogabrielgomez/kanpachi:0.6.9
          restartPolicy: Always          # this is what makes it a sidecar
          env:
            - name: KANPACHI_SEED
              value: kanpachi.accentio.dev
            - name: KANPACHI_GAME
              value: project-zomboid
            - name: KANPACHI_ROOM_NAME
              value: Los panas
          securityContext:
            capabilities:
              add: [NET_ADMIN, NET_RAW]
          volumeMounts:
            - name: kanpachi-data
              mountPath: /var/lib/kanpachi
            - name: tun
              mountPath: /dev/net/tun
      containers:
        - name: zomboid
          image: your/game-server:latest
      volumes:
        - name: kanpachi-data
          persistentVolumeClaim:
            claimName: kanpachi-data
        - name: tun
          hostPath:
            path: /dev/net/tun
            type: CharDevice
```

`NET_ADMIN` and `NET_RAW` are both needed: the first to build the adapter and
write nftables rules, the second for the probes that measure the path.

## The volume is the room

The PVC at `/var/lib/kanpachi` is what makes the code survive a restart. Delete
it and the next start opens a **different room with a different code**, and
every link you handed out stops working.

`ReadWriteOnce` is enough and correct. Two replicas sharing one room is not a
thing: a room is one adapter, one engine and one identity, and the registry pins
the identity to the code on first publish.

## Quarantine stays off

Kanpachi Protection's base quarantine writes to `/etc`, which is ephemeral in a
pod, and its reach is the whole pod network namespace rather than the adapter.
Leave `KANPACHI_QUARANTINE` off, which is the default in the image.

## Read the log

In a container Kanpachi writes to standard output as well as to its file, so
the ordinary command works:

```sh
kubectl -n <namespace> logs <pod> -c kanpachi -f
```

Room state changes, joins, leaves and connection quality all show up there.

## Add a probe

Kubernetes has no way of knowing whether the room admits anybody. A pod whose
room is dead reports `Running` and `Ready` for as long as you let it: that
happened for thirty-three hours on 2026-08-25, and nothing outside the pod said
so.

```yaml
          livenessProbe:
            exec:
              command: ["kanpachi", "status", "--json"]
            initialDelaySeconds: 90
            periodSeconds: 60
```

`kanpachi status` fails when the daemon is not answering on its socket, which is
the failure a restart actually fixes. For the room's own health, `kanpachi
exposure` names what is wrong, including a room that has members and no open
channels.

## When something is wrong

```sh
kubectl -n <namespace> exec <pod> -c kanpachi -- kanpachi doctor
kubectl -n <namespace> exec <pod> -c kanpachi -- kanpachi status
kubectl -n <namespace> exec <pod> -c kanpachi -- kanpachi exposure
```

`doctor` checks the TUN device, the kernel, the control channel and the engine.
`exposure` says which ports are open toward whom, and which alerts are up.

If people cannot join, the two questions worth asking in order are whether the
guest reaches the mesh at all, which the mesh lines in the log answer, and
whether the host's gate lists their address:

```sh
kubectl -n <namespace> exec <pod> -c kanpachi -- nft list chain inet kanpachi gate
```

## See also

- [Run a game server with Docker](run-a-game-server-docker.md), which this page
  builds on.
- [Every command](reference-cli.md), including `profile` and the game ids.
- [Kanpachi Protection](../../kanpachi-protection.md): the promise and where it
  stops.
