package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"daeva-meter/aion2"
	"daeva-meter/sniffer"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CaptureState is what the frontend gets from CaptureStatus().
type CaptureState struct {
	Running bool          `json:"running"`
	Error   string        `json:"error"`
	Stats   sniffer.Stats `json:"stats"`
	LogPath string        `json:"logPath"`
}

type App struct {
	ctx context.Context

	mu       sync.Mutex
	sn       *sniffer.Sniffer
	combat   atomic.Pointer[aion2.Combat]
	parser   atomic.Pointer[aion2.Parser]
	combatCh chan struct{}
	lastErr  string

	logMu     sync.Mutex
	logFile   *bufio.Writer
	logHandle *os.File
	logPath   string
	logCh     chan struct{}
	capStart  time.Time
}

// NewApp creates the application struct bound to the Wails frontend.
func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.openSessionLog() // no-op unless built with the dev tag
	if err := a.StartCapture(); err != nil {
		log.Printf("[app] capture auto-start failed: %v", err)
	}
}

func (a *App) shutdown(context.Context) {
	a.StopCapture()
	a.closeSessionLog()
}

// StartCapture starts the passive packet capture. No-op if already running.
// Errors (e.g. Npcap missing) are stored and returned to the UI.
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
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "capture:stats", st)
			}
		},
		OnLog: func(msg string) {
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, "capture:log", msg)
			}
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

// onPayload is called by the sniffer for every reassembled chunk. It logs the
// chunk and feeds server->client bytes into the protocol parser. It must not
// take a.mu: StopCapture holds it while waiting for the sniffer goroutines.
func (a *App) onPayload(streamID int64, dir sniffer.Direction, ts time.Time, b []byte) {
	a.writePayload(streamID, dir, ts, b)
	if dir != sniffer.ServerToClient {
		return
	}
	if p := a.parser.Load(); p != nil {
		p.Feed(streamID, b, ts)
	}
}

// emitCombat pushes a combat snapshot to the UI twice per second.
func (a *App) emitCombat(stop chan struct{}) {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if a.ctx == nil {
				continue
			}
			c := a.combat.Load()
			if c == nil {
				continue
			}
			runtime.EventsEmit(a.ctx, "combat:update", c.Snapshot(time.Now()))
		}
	}
}

// CombatState returns the current encounter (DPS board) for the frontend.
func (a *App) CombatState() aion2.Snapshot {
	if c := a.combat.Load(); c != nil {
		return c.Snapshot(time.Now())
	}
	return aion2.Snapshot{}
}

// writePayload appends one reassembled chunk to the session log. Called from
// the sniffer read goroutines; logMu guards the writer against the periodic
// flusher and shutdown.
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

// openSessionLog starts the per-app session log when session logs are enabled
// (dev builds). It records every reassembled chunk of the session in a single
// file that survives capture stop/start and is closed only at shutdown.
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

// openSessionLogFile creates captures/session-<timestamp>.txt for this run.
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

// flushSessionLog periodically flushes the buffered log so a long session is
// not lost if the process is killed.
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

func (a *App) emitLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[app] %s", msg)
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "capture:log", msg)
	}
}

// StopCapture stops the capture if running. The session log stays open: it
// spans the whole app run and is closed only at shutdown.
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

// closeSessionLog flushes and closes the session log (called at shutdown).
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

// CaptureStatus returns the current capture status for the UI.
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
