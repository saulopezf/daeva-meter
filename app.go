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
	// Dev indica que la app se compiló en modo desarrollo. El frontend lo usa
	// para mostrar el panel de diagnóstico solo en builds de desarrollo.
	Dev bool `json:"dev"`
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

	// mainWindow es la ventana overlay. clickThrough indica si ignora el ratón
	// para que los clics lleguen al juego; trayToggle es el checkbox del menú de
	// bandeja que refleja ese estado. hotkeyStop detiene el sondeo del atajo.
	mainWindow   *application.WebviewWindow
	clickThrough atomic.Bool
	trayToggle   *application.MenuItem
	hotkeyStop   chan struct{}
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
	a.hotkeyStop = make(chan struct{})
	go watchClickThroughHotkey(a.hotkeyStop, a.ToggleClickThrough)
	return nil
}

// ServiceShutdown se ejecuta al cerrar la aplicación: detiene la captura y
// cierra el log de sesión.
func (a *App) ServiceShutdown() error {
	if a.hotkeyStop != nil {
		close(a.hotkeyStop)
		a.hotkeyStop = nil
	}
	a.StopCapture()
	a.closeSessionLog()
	return nil
}

// ClickThrough indica si la ventana principal deja pasar los clics al juego.
func (a *App) ClickThrough() bool {
	return a.clickThrough.Load()
}

// SetClickThrough activa o desactiva el modo click-through de la ventana
// principal: con él activo la ventana ignora el ratón y los clics llegan al
// juego. Se desactiva con el atajo global Ctrl+Alt+M o desde la bandeja.
func (a *App) SetClickThrough(on bool) {
	a.clickThrough.Store(on)
	if a.mainWindow != nil {
		a.mainWindow.SetIgnoreMouseEvents(on)
	}
	if a.trayToggle != nil {
		a.trayToggle.SetChecked(on)
	}
	a.emit("ui:clickthrough", on)
}

// ToggleClickThrough alterna el modo click-through.
func (a *App) ToggleClickThrough() {
	a.SetClickThrough(!a.clickThrough.Load())
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

	// El modelo de combate sobrevive a parar/arrancar la captura para que el
	// historial de la sesión no se pierda hasta cerrar la aplicación.
	c := a.combat.Load()
	if c == nil {
		c = aion2.NewCombat()
		a.combat.Store(c)
	}
	a.parser.Store(aion2.NewParser(c))

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
// segundo y avisa cuando el historial gana una entrada. Se detiene cuando se
// cierra el canal stop.
func (a *App) emitCombat(stop chan struct{}) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	lastHistory := -1
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			c := a.combat.Load()
			if c == nil {
				continue
			}
			now := time.Now()
			a.emit("combat:update", c.Snapshot(now))
			history := c.History(now)
			if len(history) != lastHistory {
				lastHistory = len(history)
				a.emit("combat:history", history)
			}
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

// CombatStateFor devuelve un combate archivado. id <= 0 es el actual.
func (a *App) CombatStateFor(id int) aion2.Snapshot {
	if c := a.combat.Load(); c != nil {
		return c.SnapshotFor(time.Now(), id)
	}
	return aion2.Snapshot{}
}

// CombatHistory devuelve los combates anteriores de la sesión.
func (a *App) CombatHistory() []aion2.EncounterSummary {
	if c := a.combat.Load(); c != nil {
		return c.History(time.Now())
	}
	return []aion2.EncounterSummary{}
}

// MemberDetail devuelve el desglose por habilidad de una fila de la tabla para
// la métrica y el combate indicados. encounterID <= 0 usa el combate actual.
func (a *App) MemberDetail(encounterID int, key, metric string) aion2.MemberDetail {
	if c := a.combat.Load(); c != nil {
		return c.MemberDetailFor(time.Now(), encounterID, key, aion2.Metric(metric))
	}
	return aion2.MemberDetail{Key: key, Metric: aion2.Metric(metric), Skills: []aion2.SkillStat{}}
}

// OpenMemberWindow abre la ventana de detalle de un miembro. Si ya existe una
// ventana para ese miembro, métrica y combate, la enfoca en lugar de crear otra.
// encounterID <= 0 representa el combate actual.
func (a *App) OpenMemberWindow(encounterID int, key, name, metric string) {
	if a.app == nil || key == "" {
		return
	}
	if metric != string(aion2.MetricHealing) && metric != string(aion2.MetricTaken) {
		metric = string(aion2.MetricDamage)
	}

	a.winMu.Lock()
	defer a.winMu.Unlock()

	title := "daeva-meter"
	if name != "" {
		title += " — " + name
	}
	windowName := detailWindowName(key, encounterID, metric)
	if win, ok := a.app.Window.GetByName(windowName); ok {
		win.SetTitle(title)
		win.Show()
		win.Focus()
		return
	}

	target := fmt.Sprintf("/?member=%s&encounter=%d&metric=%s",
		url.QueryEscape(key), encounterID, url.QueryEscape(metric))
	if name != "" {
		target += "&name=" + url.QueryEscape(name)
	}
	opts := application.WebviewWindowOptions{
		Name:             windowName,
		Title:            title,
		Width:            detailWidth,
		Height:           detailHeight,
		MinWidth:         560,
		MinHeight:        360,
		Frameless:        true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              target,
	}
	// La ventana principal es topmost, así que los detalles se abren a su lado
	// para que no queden ocultos debajo de ella.
	if x, y, ok := a.detailPlacement(); ok {
		opts.InitialPosition = application.WindowXY
		opts.X = x
		opts.Y = y
	}
	a.app.Window.NewWithOptions(opts)
}

// Tamaño de las ventanas de detalle y separación respecto a la principal.
const (
	detailWidth  = 680
	detailHeight = 520
	detailGap    = 12
)

// detailPlacement calcula una posición para la ventana de detalle al lado de la
// ventana principal, dentro del área de trabajo de su pantalla. Devuelve false
// si no existe la ventana principal, en cuyo caso se usa la posición por
// defecto de Wails (centrada).
func (a *App) detailPlacement() (int, int, bool) {
	mainWin, ok := a.app.Window.GetByName("main")
	if !ok || mainWin == nil {
		return 0, 0, false
	}

	mx, my := mainWin.Position()
	mw, _ := mainWin.Size()
	x := mx + mw + detailGap
	y := my

	if screen, err := mainWin.GetScreen(); err == nil && screen != nil {
		area := screen.WorkArea
		// Si no cabe a la derecha, se coloca a la izquierda de la principal.
		if x+detailWidth > area.X+area.Width {
			x = mx - detailWidth - detailGap
		}
		// Ajuste final para no salirse del área de trabajo.
		if x < area.X {
			x = area.X
		}
		if x+detailWidth > area.X+area.Width {
			x = area.X + area.Width - detailWidth
		}
		if y < area.Y {
			y = area.Y
		}
		if y+detailHeight > area.Y+area.Height {
			y = area.Y + area.Height - detailHeight
		}
		if y < area.Y {
			y = area.Y
		}
	}

	return x, y, true
}

// detailWindowName genera un nombre estable y seguro para la ventana de detalle
// a partir de la clave del miembro, el combate y la métrica.
func detailWindowName(key string, encounterID int, metric string) string {
	h := fnv.New32a()
	fmt.Fprintf(h, "%s|%d|%s", key, encounterID, metric)
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

// StopCapture detiene la captura si está en marcha. El log de sesión y el
// historial de combates siguen vivos: solo se cierran en el apagado.
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
	// El modelo de combate no se descarta: el historial debe sobrevivir a un
	// reinicio de la captura. Se cierra el combate en curso para que el
	// siguiente golpe abra uno nuevo.
	if c := a.combat.Load(); c != nil {
		c.CloseEncounter()
		now := time.Now()
		a.emit("combat:update", c.Snapshot(now))
		a.emit("combat:history", c.History(now))
	}
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
	st := CaptureState{Error: a.lastErr, Dev: devMode}
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
