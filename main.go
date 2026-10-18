// daeva-meter es un medidor de DPS para Aion 2. Captura de forma pasiva el
// tráfico TCP del juego (puerto 13328) mediante Npcap, reensambla los streams,
// decodifica el protocolo del servidor y muestra en pantalla el daño del grupo.
//
// Es una aplicación de escritorio Wails v2: este paquete contiene el backend Go
// (main.go, app.go) y embebe el frontend React ya compilado (frontend/dist).
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// assets contiene el frontend ya compilado (frontend/dist) embebido en el
// binario, de modo que la aplicación no depende de archivos externos en tiempo
// de ejecución.
//
//go:embed all:frontend/dist
var assets embed.FS

// main es el punto de entrada de la aplicación Wails. Registra la instancia de
// App (cuyos métodos públicos quedan expuestos al frontend) y abre la ventana
// con los callbacks de ciclo de vida startup/shutdown.
func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "daeva-meter",
		Width:  480,
		Height: 420,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
