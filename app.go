package main

import (
	"bufio"
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"daeva-meter/aion2"
	"daeva-meter/sniffer"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// CaptureState es el estado que el frontend obtiene de CaptureStatus().
type CaptureState struct {
	Running bool          `json:"running"`
	Error   string        `json:"error"`
	Stats   sniffer.Stats `json:"stats"`
	LogPath string        `json:"logPath"`
}

// App es el backend de la aplicación. Coordina el sniffer (captura de red), el
// parser de protocolo y el modelo de combate, y expone el estado resultante al
// frontend a través de eventos y métodos públicos.
//
// Concurrencia:
//   - mu protege sn/combatCh/lastErr contra StopCapture y CaptureStatus.
//   - combat y parser son punteros atómicos: se leen desde los goroutines del
//     sniffer sin bloquear.
//   - logMu protege el escritor del log de sesión (varias goroutines de lectura
//     del sniffer escriben en él mientras un goroutine lo vacía periódicamente).
//   - winMu serializa la creación de ventanas de detalle.
type App struct {
	app *application.App

	mu       sync.Mutex
	sn       *sniffer.Sniffer
	combat   atomic.Pointer[aion2.Combat]
	parser   atomic.Pointer[aion2.Parser]
	combatCh chan struct{}
	lastErr  string

	// winMu serializa la creación de ventanas de detalle para que dos clics
	// rápidos sobre el mismo miembro no abran ventanas duplicadas.
	winMu sync.Mutex

	logMu     sync.Mutex
	logFile   *bufio.Writer
	logHandle *os.File
	logPath   string
	logCh     chan struct{}
	capStart  time.Time
}

// NewApp crea la estructura de la aplicación que se enlaza con el frontend.
func NewApp() *App {
	return &App{}
}

// emit publica un evento hacia el frontend. Es seguro llamarlo desde los
// goroutines del sniffer: app se asigna antes de arrancar la aplicación y el
// emisor de eventos de Wails es concurrente.
func (a *App) emit(name string, data any) {
	if a.app != nil {
		a.app.Event.Emit(name, data)
	}
}

// ServiceStartup se ejecuta al arrancar la aplicación: abre el log de sesión (si
// está habilitado) y arranca la captura automáticamente.
func (a *App) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	a.openSessionLog() // no hace nada salvo en compilaciones de desarrollo
	if err := a.StartCapture(); err != nil {
		log.Printf("[app] capture auto-start failed: %v", err)
	}
	return nil
}

// ServiceShutdown se ejecuta al cerrar la aplicación: detiene la captura y
// cierra el log de sesión.
func (a *App) ServiceShutdown() error {
	a.StopCapture()
	a.closeSessionLog()
	return nil
}

// StartCapture arranca la captura pasiva de paquetes. No hace nada si ya está en
// marcha. Cualquier error (p. ej. Npcap no instalado) se guarda para que la
// interfaz lo muestre.
func (a *App) StartCapture() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sn != nil {
		return nil
	}

	a.combat.Store(aion2.NewCombat())
	a.parser.Store(aion2.NewParser(a.combat.Load()))

	s := sniffer.New(sniffer.Options{
		Port:    sniffer.DefaultPort,
		Console: true,
		OnStats: func(st sniffer.Stats) {
			a.emit("capture:stats", st)
		},
		OnLog: func(msg string) {
			a.emit("capture:log", msg)
		},
		OnPayload: a.onPayload,
	})

	if err := s.Start(); err != nil {
		a.combat.Store(nil)
		a.parser.Store(nil)
		a.lastErr = err.Error()
		return err
	}
	a.sn = s
	a.lastErr = ""
	a.combatCh = make(chan struct{})
	go a.emitCombat(a.combatCh)
	return nil
}

// onPayload lo llama el sniffer por cada fragmento reensamblado. Registra el
// fragmento y entrega los bytes servidor->cliente al parser del protocolo.
//
// Importante: no debe tomar a.mu, porque StopCapture retiene ese mutex mientras
// espera a que terminen los goroutines del sniffer.
func (a *App) onPayload(streamID int64, dir sniffer.Direction, ts time.Time, b []byte) {
	a.writePayload(streamID, dir, ts, b)
	if dir != sniffer.ServerToClient {
		return
	}
	if p := a.parser.Load(); p != nil {
		p.Feed(streamID, b, ts)
	}
}

// emitCombat empuja una instantánea del combate a la interfaz dos veces por
// segundo. Se detiene cuando se cierra el canal stop.
func (a *App) emitCombat(stop chan struct{}) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			c := a.combat.Load()
			if c == nil {
				continue
			}
			a.emit("combat:update", c.Snapshot(time.Now()))
		}
	}
}

// CombatState devuelve el encuentro actual (tabla de DPS) para el frontend.
func (a *App) CombatState() aion2.Snapshot {
	if c := a.combat.Load(); c != nil {
		return c.Snapshot(time.Now())
	}
	return aion2.Snapshot{}
}

// MemberDetail devuelve el desglose por habilidad de una fila de la tabla.
func (a *App) MemberDetail(key string) aion2.MemberDetail {
	if c := a.combat.Load(); c != nil {
		return c.MemberDetail(time.Now(), key)
	}
	return aion2.MemberDetail{Key: key, Skills: []aion2.SkillStat{}}
}

// OpenMemberWindow abre la ventana de detalle de un miembro. Si ya existe una
// ventana para ese miembro, la enfoca en lugar de crear otra.
func (a *App) OpenMemberWindow(key, name string) {
	if a.app == nil || key == "" {
		return
	}

	a.winMu.Lock()
	defer a.winMu.Unlock()

	title := "daeva-meter"
	if name != "" {
		title += " — " + name
	}
	windowName := detailWindowName(key)
	if win, ok := a.app.Window.GetByName(windowName); ok {
		win.SetTitle(title)
		win.Show()
		win.Focus()
		return
	}

	target := "/?member=" + url.QueryEscape(key)
	if name != "" {
		target += "&name=" + url.QueryEscape(name)
	}
	a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             windowName,
		Title:            title,
		Width:            680,
		Height:           520,
		MinWidth:         560,
		MinHeight:        360,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              target,
	})
}

// detailWindowName genera un nombre estable y seguro para la ventana de detalle
// a partir de la clave del miembro.
func detailWindowName(key string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf("detail-%08x", h.Sum32())
}

// writePayload añade un fragmento reensamblado al log de sesión. Lo llaman los
// goroutines de lectura del sniffer; logMu protege el escritor frente al
// vaciado periódico del buffer y al cierre en el apagado.
func (a *App) writePayload(streamID int64, dir sniffer.Direction, ts time.Time, b []byte) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.logFile == nil {
		return
	}
	rel := ts.Sub(a.capStart).Seconds()
	if rel < 0 {
		rel = 0
	}
	fmt.Fprintf(a.logFile, "+%.6f %s stream=%d len=%d %x\n",
		rel, dir, streamID, len(b), b)
}

// openSessionLog inicia el log de sesión cuando los logs están habilitados
// (compilaciones de desarrollo). Registra cada fragmento reensamblado de la
// sesión en un único archivo que sobrevive a paradas/reinicios de la captura y
// solo se cierra en el apagado.
func (a *App) openSessionLog() {
	if !sessionLogs {
		return
	}
	path, f, w, err := openSessionLogFile()
	if err != nil {
		log.Printf("[app] open session log: %v", err)
		return
	}
	a.logMu.Lock()
	a.logFile, a.logHandle, a.logPath = w, f, path
	a.logMu.Unlock()
	a.capStart = time.Now()
	a.logCh = make(chan struct{})
	go a.flushSessionLog(a.logCh)
	a.emitLogf("session log: %s", path)
}

// openSessionLogFile crea captures/session-<timestamp>.txt para esta ejecución.
func openSessionLogFile() (string, *os.File, *bufio.Writer, error) {
	dir := "captures"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, nil, fmt.Errorf("create captures dir: %w", err)
	}
	name := filepath.Join(dir, fmt.Sprintf("session-%s.txt", time.Now().Format("20060102-150405")))
	f, err := os.Create(name)
	if err != nil {
		return "", nil, nil, fmt.Errorf("create session log: %w", err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	fmt.Fprintf(w, "# started %s port=%d\n", time.Now().UTC().Format(time.RFC3339), sniffer.DefaultPort)
	return name, f, w, nil
}

// flushSessionLog vacía periódicamente el buffer del log para no perder una
// sesión larga si el proceso se mata.
func (a *App) flushSessionLog(stop chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.logMu.Lock()
			if a.logFile != nil {
				_ = a.logFile.Flush()
			}
			a.logMu.Unlock()
		}
	}
}

// emitLogf escribe un mensaje a la consola y, si hay ventana, lo publica como
// evento "capture:log" para el frontend.
func (a *App) emitLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[app] %s", msg)
	a.emit("capture:log", msg)
}

// StopCapture detiene la captura si está en marcha. El log de sesión sigue
// abierto: abarca toda la ejecución de la app y solo se cierra en el apagado.
func (a *App) StopCapture() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.combatCh != nil {
		close(a.combatCh)
		a.combatCh = nil
	}
	if a.sn != nil {
		a.sn.Stop()
		a.sn = nil
	}
	a.combat.Store(nil)
	a.parser.Store(nil)
}

// closeSessionLog vacía y cierra el log de sesión (se llama en el apagado).
func (a *App) closeSessionLog() {
	if a.logCh != nil {
		close(a.logCh)
		a.logCh = nil
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.logFile == nil {
		return
	}
	if err := a.logFile.Flush(); err != nil {
		log.Printf("[app] flush session log: %v", err)
	}
	if a.logHandle != nil {
		a.logHandle.Close()
	}
	a.logFile = nil
	a.logHandle = nil
	if a.logPath != "" {
		a.emitLogf("session log closed: %s", a.logPath)
	}
}

// CaptureStatus devuelve el estado actual de la captura para la interfaz.
func (a *App) CaptureStatus() CaptureState {
	a.mu.Lock()
	st := CaptureState{Error: a.lastErr}
	if a.sn != nil {
		st.Running = a.sn.Running()
		st.Stats = a.sn.Stats()
	}
	a.mu.Unlock()

	a.logMu.Lock()
	st.LogPath = a.logPath
	a.logMu.Unlock()
	return st
}
