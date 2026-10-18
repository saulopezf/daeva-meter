package sniffer

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/tcpassembly"
)

// DefaultPort es el puerto TCP por el que los clientes de Aion 2 hablan con su
// servidor.
const DefaultPort uint16 = 13328

// Direction es el sentido de un payload TCP reensamblado respecto al servidor
// del juego.
type Direction uint8

const (
	// UnknownDir se usa cuando ningún extremo del flujo emplea DefaultPort.
	UnknownDir Direction = iota
	// ClientToServer es el tráfico hacia el puerto del servidor del juego.
	ClientToServer
	// ServerToClient es el tráfico que sale del puerto del servidor del juego.
	ServerToClient
)

// String devuelve la etiqueta corta de la dirección usada en los logs.
func (d Direction) String() string {
	switch d {
	case ClientToServer:
		return "C2S"
	case ServerToClient:
		return "S2C"
	default:
		return "???"
	}
}

const (
	snapLen     = 262144                 // bytes capturados por paquete
	readTimeout = 500 * time.Millisecond // tiempo de espera de lectura de pcap
	previewLen  = 64                     // bytes mostrados en la vista previa
	tickEvery   = 250 * time.Millisecond // cadencia del bucle de estadísticas
)

// Stats es una instantánea de los contadores de captura, codificada en JSON para
// el frontend.
type Stats struct {
	Devices       int     `json:"devices"`
	Packets       int64   `json:"packets"`
	Bytes         int64   `json:"bytes"`
	PayloadBytes  int64   `json:"payloadBytes"`
	Streams       int64   `json:"streams"`
	Gaps          int64   `json:"gaps"`
	PacketsPerSec float64 `json:"packetsPerSec"`
	BytesPerSec   float64 `json:"bytesPerSec"`
	TrafficSeen   bool    `json:"trafficSeen"`
}

// Options configura un Sniffer.
type Options struct {
	Port    uint16       // puerto TCP a filtrar; DefaultPort si es 0
	Console bool         // imprime una línea de resumen por segundo en stdout
	OnStats func(Stats)  // se llama en cada tick con las tasas actuales
	OnLog   func(string) // se llama por cada línea de log (incluidas las de consola)
	// OnPayload se llama por cada fragmento de payload reensamblado, con el
	// sentido del fragmento respecto al puerto del servidor. El slice de bytes
	// solo es válido durante la llamada.
	OnPayload func(streamID int64, dir Direction, ts time.Time, b []byte)
}

// Sniffer captura de forma pasiva el tráfico TCP del puerto Options.Port en
// todos los dispositivos que Npcap pueda abrir, reensambla los streams y reporta
// contadores y logs.
//
// Una instancia de Sniffer es de un solo uso: crea una nueva por cada Start.
type Sniffer struct {
	opts Options

	mu      sync.Mutex
	asm     *tcpassembly.Assembler
	handles []*pcap.Handle

	stopCh chan struct{}
	wg     sync.WaitGroup

	running      atomic.Bool
	trafficSeen  atomic.Bool
	packets      atomic.Int64
	bytes        atomic.Int64
	payloadBytes atomic.Int64
	streams      atomic.Int64
	gaps         atomic.Int64

	devices int
}

// New construye un sniffer detenido con las opciones dadas.
func New(opts Options) *Sniffer {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	return &Sniffer{opts: opts}
}

// Start abre handles de captura en todos los dispositivos disponibles y lanza
// los goroutines de lectura y de estadísticas. Después es seguro llamar a Stop
// en paralelo.
//
// Abre cada interfaz con el filtro BPF "tcp port N"; las que fallen se anotan y
// se omiten. Si ninguna se pudo abrir (típicamente porque falta Npcap), devuelve
// un error y deja el sniffer detenido.
func (s *Sniffer) Start() error {
	if !s.running.CompareAndSwap(false, true) {
		return errors.New("sniffer already running")
	}
	s.stopCh = make(chan struct{})

	filter := fmt.Sprintf("tcp port %d", s.opts.Port)

	devs, err := pcap.FindAllDevs()
	if err != nil {
		s.running.Store(false)
		return fmt.Errorf("enumerate devices: %w", err)
	}

	var opened []*pcap.Handle
	for _, d := range devs {
		h, err := openHandle(d.Name)
		if err != nil {
			s.logf("device %q: open failed: %v", displayName(d), err)
			continue
		}
		if err := h.SetBPFFilter(filter); err != nil {
			h.Close()
			s.logf("device %q: filter %q failed: %v", displayName(d), filter, err)
			continue
		}
		opened = append(opened, h)
		s.logf("listening on %q (%s)", displayName(d), filter)
	}
	if len(opened) == 0 {
		s.running.Store(false)
		return errors.New("could not open any capture device (is Npcap installed and running?)")
	}
	s.handles = opened
	s.devices = len(opened)

	pool := tcpassembly.NewStreamPool(&streamFactory{s: s})
	s.asm = tcpassembly.NewAssembler(pool)

	for i, h := range opened {
		s.wg.Add(1)
		go func(id int, h *pcap.Handle) {
			defer s.wg.Done()
			s.readLoop(id, h)
		}(i, h)
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.tickLoop()
	}()

	s.logf("capture started on %d device(s), port %d", len(opened), s.opts.Port)
	return nil
}

// Stop cierra todos los handles y espera a que terminen los goroutines.
func (s *Sniffer) Stop() {
	if !s.running.CompareAndSwap(true, false) {
		return
	}
	close(s.stopCh)
	for _, h := range s.handles {
		h.Close()
	}
	s.wg.Wait()
	s.handles = nil
	s.asm = nil
	s.logf("capture stopped (%d packets, %s total)", s.packets.Load(), humanBytes(s.bytes.Load()))
}

// Running indica si el sniffer está activo.
func (s *Sniffer) Running() bool {
	return s.running.Load()
}

// Stats devuelve los contadores acumulados actuales (las tasas vienen a 0; las
// tasas en vivo llegan por Options.OnStats).
func (s *Sniffer) Stats() Stats {
	return s.snapshot(0, 0)
}

func (s *Sniffer) snapshot(ratePkt, rateBytes float64) Stats {
	return Stats{
		Devices:       s.devices,
		Packets:       s.packets.Load(),
		Bytes:         s.bytes.Load(),
		PayloadBytes:  s.payloadBytes.Load(),
		Streams:       s.streams.Load(),
		Gaps:          s.gaps.Load(),
		PacketsPerSec: ratePkt,
		BytesPerSec:   rateBytes,
		TrafficSeen:   s.trafficSeen.Load(),
	}
}

// openHandle abre un handle de captura en vivo, reintentando sin modo
// promiscuo si el driver lo rechaza.
func openHandle(device string) (*pcap.Handle, error) {
	h, err := pcap.OpenLive(device, snapLen, true, readTimeout)
	if err == nil {
		return h, nil
	}
	h2, err2 := pcap.OpenLive(device, snapLen, false, readTimeout)
	if err2 != nil {
		return nil, fmt.Errorf("promisc: %v; non-promisc: %w", err, err2)
	}
	return h2, nil
}

// readLoop lee paquetes de un handle hasta que se cierra el handle o Stop. Los
// timeouts de pcap son normales y se ignoran; un error real solo se reporta si
// no venía precedido de una parada.
func (s *Sniffer) readLoop(id int, h *pcap.Handle) {
	src := gopacket.NewPacketSource(h, h.LinkType())
	src.DecodeOptions.Lazy = true
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}
		pkt, err := src.NextPacket()
		if err != nil {
			if errors.Is(err, pcap.NextErrorTimeoutExpired) {
				continue
			}
			select {
			case <-s.stopCh:
				// handle cerrado por Stop, esperado.
			default:
				s.logf("device %d read error: %v", id, err)
			}
			return
		}
		s.handlePacket(pkt)
	}
}

// handlePacket actualiza contadores y entrega el paquete TCP al ensamblador. La
// llamada a AssembleWithTimestamp va bajo s.mu porque el ensamblador no es
// seguro para uso concurrente entre los distintos dispositivos.
func (s *Sniffer) handlePacket(pkt gopacket.Packet) {
	s.packets.Add(1)
	s.bytes.Add(int64(len(pkt.Data())))
	if !s.trafficSeen.Load() && s.trafficSeen.CompareAndSwap(false, true) {
		s.logf("traffic detected on port %d", s.opts.Port)
	}

	tcpL := pkt.Layer(layers.LayerTypeTCP)
	if tcpL == nil {
		return
	}
	tcp := tcpL.(*layers.TCP)
	netL := pkt.NetworkLayer()
	if netL == nil {
		return
	}
	ts := pkt.Metadata().Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	s.mu.Lock()
	s.asm.AssembleWithTimestamp(netL.NetworkFlow(), tcp, ts)
	s.mu.Unlock()
}

// tickLoop publica estadísticas a la interfaz en cada tick e imprime un resumen
// por consola una vez por segundo cuando Options.Console está activo. Además,
// cada segundo descarta los streams inactivos para que el ensamblador no acumule
// conexiones muertas.
func (s *Sniffer) tickLoop() {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()

	var lastPkt, lastBytes int64
	var secPkt, secBytes int64
	n := 0

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			n++
			pkt := s.packets.Load()
			by := s.bytes.Load()

			perSec := 1.0 / tickEvery.Seconds()
			if s.opts.OnStats != nil {
				s.opts.OnStats(s.snapshot(
					float64(pkt-lastPkt)*perSec,
					float64(by-lastBytes)*perSec,
				))
			}
			lastPkt, lastBytes = pkt, by

			if n%4 == 0 {
				// Descarta los streams con más de 30s de inactividad para que
				// el ensamblador no acumule conexiones muertas para siempre.
				s.mu.Lock()
				s.asm.FlushOlderThan(time.Now().Add(-30 * time.Second))
				s.mu.Unlock()
			}

			if s.opts.Console && n%4 == 0 {
				dPkt := pkt - secPkt
				dBy := by - secBytes
				secPkt, secBytes = pkt, by
				state := "idle"
				if s.trafficSeen.Load() {
					state = "traffic"
				}
				log.Printf("[sniffer] pkts=%d (+%d/s) bytes=%s (+%s/s) payload=%s streams=%d gaps=%d devs=%d %s",
					pkt, dPkt,
					humanBytes(by), humanBytes(dBy),
					humanBytes(s.payloadBytes.Load()),
					s.streams.Load(), s.gaps.Load(),
					s.devices, state)
			}
		}
	}
}

// logf escribe un mensaje a la consola y, si hay callback, lo entrega a OnLog.
func (s *Sniffer) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[sniffer] %s", msg)
	if s.opts.OnLog != nil {
		s.opts.OnLog(msg)
	}
}

// displayName devuelve la descripción legible de una interfaz, o su nombre.
func displayName(d pcap.Interface) string {
	if d.Description != "" {
		return d.Description
	}
	return d.Name
}

// humanBytes formatea un número de bytes en B/KiB/MiB.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
