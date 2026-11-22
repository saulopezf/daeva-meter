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

// trayIcon es el icono de la aplicación que se muestra en la bandeja del sistema.
//
//go:embed build/appicon.png
var trayIcon []byte

// init registra el tipo de dato de los eventos personalizados. El generador de
// bindings lo usa para producir una API TypeScript tipada para cada evento.
func init() {
	application.RegisterEvent[sniffer.Stats]("capture:stats")
	application.RegisterEvent[string]("capture:log")
	application.RegisterEvent[aion2.Snapshot]("combat:update")
	application.RegisterEvent[[]aion2.EncounterSummary]("combat:history")
	application.RegisterEvent[bool]("ui:clickthrough")
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

	// La ventana es un overlay frameless (el marco lo dibuja el frontend) con
	// fondo transparente: el frontend pinta un fondo semitransparente cuya
	// opacidad regula el usuario. Se mantiene siempre encima del juego.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "daeva-meter",
		Width:            300,
		Height:           120,
		MinWidth:         240,
		MinHeight:        60,
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
	})
	backend.mainWindow = mainWindow

	// Icono de bandeja: es la vía (junto al atajo Ctrl+Alt+M) para salir del
	// modo click-through, en el que la ventana no recibe clics.
	trayMenu := application.NewMenu()
	backend.trayToggle = trayMenu.AddCheckbox("Click-through (Ctrl+Alt+M)", false)
	backend.trayToggle.OnClick(func(*application.Context) {
		backend.ToggleClickThrough()
	})
	trayMenu.Add("Mostrar / ocultar").OnClick(func(*application.Context) {
		if mainWindow.IsVisible() {
			mainWindow.Hide()
		} else {
			mainWindow.Show()
		}
	})
	trayMenu.AddSeparator()
	trayMenu.Add("Salir").OnClick(func(*application.Context) {
		mainWindow.Close()
	})
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("daeva-meter")
	tray.SetMenu(trayMenu)
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
