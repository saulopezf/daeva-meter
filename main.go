// daeva-meter es un medidor de DPS para Aion 2. Captura de forma pasiva el
// tráfico TCP del juego (puerto 13328) mediante Npcap, reensambla los streams,
// decodifica el protocolo del servidor y muestra en pantalla el daño del grupo.
//
// Es una aplicación de escritorio Wails v3: este paquete contiene el backend Go
// (main.go, app.go) y embebe el frontend React ya compilado (frontend/dist).
package main

import (
	"embed"
	"log"

	"daeva-meter/aion2"
	"daeva-meter/sniffer"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// assets contiene el frontend ya compilado (frontend/dist) embebido en el
// binario, de modo que la aplicación no depende de archivos externos en tiempo
// de ejecución.
//
//go:embed all:frontend/dist
var assets embed.FS

// init registra el tipo de dato de los eventos personalizados. El generador de
// bindings lo usa para producir una API TypeScript tipada para cada evento.
func init() {
	application.RegisterEvent[sniffer.Stats]("capture:stats")
	application.RegisterEvent[string]("capture:log")
	application.RegisterEvent[aion2.Snapshot]("combat:update")
}

// main es el punto de entrada de la aplicación Wails. Crea la aplicación,
// registra el servicio App (cuyos métodos públicos quedan expuestos al
// frontend) y abre la ventana principal.
func main() {
	backend := NewApp()

	app := application.New(application.Options{
		Name:        "daeva-meter",
		Description: "Medidor de DPS para Aion 2",
		Services: []application.Service{
			application.NewService(backend),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// El servicio necesita la aplicación para emitir eventos. Se asigna antes
	// de app.Run(), de modo que ServiceStartup ya puede usarla.
	backend.app = app

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "daeva-meter",
		Width:            480,
		Height:           420,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})
	// Al cerrar la ventana principal se cierran también las ventanas de detalle.
	//
	// No llamamos a app.Quit() aquí: Quit destruye el bucle de mensajes mientras
	// las ventanas (y sus WebView2) todavía no han terminado de cerrarse, lo que
	// hace que el proceso salga con ventanas vivas y WebView2 falle al
	// desregistrar su clase ("Failed to unregister class Chrome_WidgetWin_0.
	// Error = 1412"). Dejamos que Wails cierre la ventana principal y que
	// termine la aplicación cuando se destruya el último HWND.
	mainWindow.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		for _, win := range app.Window.GetAll() {
			if win.ID() != mainWindow.ID() {
				win.Close()
			}
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
