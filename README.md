# daeva-meter

Medidor de DPS para **Aion 2**. Lee el tráfico de red del juego y muestra en vivo
el daño de tu grupo, sin tocar ni modificar el juego.

## ¿Qué hace?

- Detecta automáticamente a tu personaje y a los miembros de tu grupo.
- Muestra una tabla de daño en tiempo real: DPS, daño total, golpes y críticos.
- Pulsa una fila para abrir el desglose por habilidad: daño, porcentaje del total,
  DPS, golpes, críticos y daño medio de cada habilidad.
- Suma el daño de mascotas y hechizos de suelo a quien los lanza.

La captura es **pasiva**: la app solo observa el tráfico de red del juego, no
interactúa con él ni necesita modificarlo.

> **Estado:** versión inicial. Por ahora mide **daño infligido** y su desglose por
> habilidad. Las habilidades se muestran como `Skill #<id>` hasta que se añadan
> nombres. Próximamente se añadirán modo curación, daño recibido y desglose por
> enemigo.

## Qué necesitas

- Windows 10/11.
- **[Npcap](https://npcap.com)** instalado (es lo que permite leer el tráfico de
  red). Es un requisito obligatorio.
- El ejecutable `daeva-meter.exe`. No está firmado digitalmente, así que Windows
  SmartScreen puede mostrar un aviso la primera vez.

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
  - **Pulsa una fila** para abrir una ventana con el desglose por habilidad. La
    ventana se actualiza en vivo y se reutiliza si vuelves a pulsar el mismo
    jugador.
- **Pie de la ventana**: contadores de red y el último mensaje de estado.

## Si no funciona

- **Se queda en `LISTENING` y no aparece nada:** normalmente es que **Npcap no
  está instalado**, o que la app no tiene permisos. Instálalo y/o ejecuta la app
  como administrador.
- **Mensaje de error en rojo:** la app no ha podido abrir ningún dispositivo de
  red; casi siempre significa que falta Npcap.
- **No aparezco en la tabla:** tu personaje se detecta al entrar en combate.
  Espera a golpear o recibir daño.

## Catálogo de habilidades

El protocolo solo proporciona el id numérico de la habilidad, así que la
interfaz muestra `Skill #<id>` cuando no hay datos. Los nombres y metadatos están
en `aion2/skill_catalog.json`, que se embebe en el ejecutable:

```json
{
  "version": 1,
  "skills": [
    {
      "id": 16040000,
      "name": "Combustion",
      "icon": "icons/16040000.png",
      "class": "Elementalist",
      "type": "active",
      "description": "Opcional"
    }
  ]
}
```

Solo `id` y `name` son obligatorios. `icon`, `class`, `type` y `description`
están reservados para metadatos futuros; por ahora la interfaz solo usa el
nombre. Los ids que no estén en el catálogo se muestran como `Skill #<id>`.

El JSON se parsea una sola vez al arrancar y se guarda en un mapa en memoria.
Las entradas inválidas o duplicadas se ignoran y se registran en el log; el resto
sigue funcionando. Un id duplicado se queda con la última entrada.

> El catálogo va embebido en el `.exe`. Después de editarlo hay que recompilar
> para que los cambios lleguen a la aplicación. Si más adelante quieres editar
> nombres sin recompilar, se puede añadir un override externo.

## Próximas mejoras

- Modo curación.
- Daño recibido.
- Daño desglosado por enemigo (el acumulador interno ya guarda el objetivo).
- Historial de peleas (no solo la pelea actual).

## Para desarrolladores

El proyecto se compila con [Wails v3](https://v3.wails.io) (backend Go + frontend
React). Requiere Go 1.26+ y Node.js.

Instala el CLI de Wails v3 (es un binario distinto de `wails` v2):

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.34
```

Comandos habituales:

```bash
wails3 dev                  # desarrollo con recarga en caliente
wails3 build                # genera bin/daeva-meter.exe para distribuir
wails3 generate bindings -ts -i  # regenera frontend/bindings desde los servicios Go
go test ./...               # tests del backend
```

Para distribuir la versión estable solo hay que compartir `bin/daeva-meter.exe`.
No se genera instalador.

La primera vez (o tras cambiar dependencias del frontend):

```bash
cd frontend && npm install
```

Los bindings generados se guardan en `frontend/bindings/` y el build de
producción usa el tag `production` (desactiva el log de sesión).
