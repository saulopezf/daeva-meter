# daeva-meter

Medidor de DPS para **Aion 2**. Lee el tráfico de red del juego y muestra en vivo
el daño de tu grupo, sin tocar ni modificar el juego.

## ¿Qué hace?

- Detecta automáticamente a tu personaje y a los miembros de tu grupo.
- Muestra una tabla de daño en tiempo real: DPS, daño total, golpes y críticos.
- Suma el daño de mascotas y hechizos de suelo a quien los lanza.

La captura es **pasiva**: la app solo observa el tráfico de red del juego, no
interactúa con él ni necesita modificarlo.

> **Estado:** versión inicial. Por ahora solo mide **daño infligido**. Próximamente
> se añadirán daño por habilidad, modo curación y daño recibido.

## Qué necesitas

- Windows 10/11.
- **[Npcap](https://npcap.com)** instalado (es lo que permite leer el tráfico de
  red). Es un requisito obligatorio.
- El ejecutable `daeva-meter.exe`.

## Cómo usarlo

1. Instala **Npcap** si aún no lo tienes.
2. Abre **daeva-meter.exe**. Puede que necesites ejecutarlo **como
   administrador** para poder leer la red.
3. La app empieza a capturar sola. Entra en Aion 2 con tu personaje.
4. Cuando empieces a pelear, aparecerás en la tabla con tu grupo.

### La ventana

- **Estado** (arriba a la izquierda):
  - `LISTENING`: está escuchando, pero aún no ha visto tráfico del juego.
  - `LIVE`: está recibiendo datos del juego.
  - `STOPPED`: la captura está detenida.
  - `ERROR`: algo ha fallado (mira el mensaje en rojo).
- **Botón Start / Stop**: inicia o detiene la captura.
- **Tabla de DPS**: una fila por jugador, ordenada de mayor a menor daño.
  - `dps` → daño por segundo.
  - el número de la derecha → daño total.
  - `YOU` marca tu propia fila.
- **Pie de la ventana**: contadores de red y el último mensaje de estado.

## Si no funciona

- **Se queda en `LISTENING` y no aparece nada:** normalmente es que **Npcap no
  está instalado**, o que la app no tiene permisos. Instálalo y/o ejecuta la app
  como administrador.
- **Mensaje de error en rojo:** la app no ha podido abrir ningún dispositivo de
  red; casi siempre significa que falta Npcap.
- **No aparezco en la tabla:** tu personaje se detecta al entrar en combate.
  Espera a golpear o recibir daño.

## Próximas mejoras

- Daño desglosado por habilidad.
- Modo curación.
- Daño recibido.
- Historial de peleas (no solo la pelea actual).

## Para desarrolladores

El proyecto se compila con [Wails](https://wails.io) (backend Go + frontend React).

```bash
wails dev      # desarrollo con recarga en caliente
wails build    # genera el ejecutable en build/bin/
go test ./...  # tests del backend
```
