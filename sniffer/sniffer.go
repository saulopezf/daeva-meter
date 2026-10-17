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

// DefaultPort is the TCP port Aion 2 clients talk to their server on.
const DefaultPort uint16 = 13328

// Direction of a reassembled TCP payload relative to the game server.
type Direction uint8

const (
	// UnknownDir is set when neither side of the flow uses DefaultPort.
	UnknownDir Direction = iota
	// ClientToServer is traffic towards the game server port.
	ClientToServer
	// ServerToClient is traffic originating from the game server port.
	ServerToClient
)

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
	snapLen     = 262144
	readTimeout = 500 * time.Millisecond
	previewLen  = 64
	tickEvery   = 250 * time.Millisecond
)

// Stats is a snapshot of capture counters, JSON-encoded for the frontend.
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

// Options configures a Sniffer.
type Options struct {
	Port    uint16       // TCP port to filter on, DefaultPort if 0
	Console bool         // print a 1s summary line to stdout
	OnStats func(Stats)  // called every tick with fresh rates
	OnLog   func(string) // called for every log line (console lines included)
	// OnPayload is called for every reassembled payload chunk with the
	// chunk direction relative to the game server port. The byte slice is
	// only valid for the duration of the call.
	OnPayload func(streamID int64, dir Direction, ts time.Time, b []byte)
}

// Sniffer passively captures TCP traffic on Options.Port from every device
// Npcap can open, reassembles the streams and reports counters/logs.
// One Sniffer instance is single-use: create a new one for every Start.
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

// New builds a stopped sniffer for the given options.
func New(opts Options) *Sniffer {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	return &Sniffer{opts: opts}
}

// Start opens capture handles on every available device and launches the
// read/stats goroutines. It is safe to call Stop concurrently afterwards.
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

// Stop closes all handles and waits for the goroutines to exit.
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

// Running reports whether the sniffer is active.
func (s *Sniffer) Running() bool {
	return s.running.Load()
}

// Stats returns the current cumulative counters (rates are 0; live rates
// arrive through Options.OnStats).
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

// openHandle opens a live capture handle, retrying without promiscuous
// mode if the driver refuses it.
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
				// handle closed by Stop, expected.
			default:
				s.logf("device %d read error: %v", id, err)
			}
			return
		}
		s.handlePacket(pkt)
	}
}

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

// tickLoop pushes stats to the UI every tick and prints a console summary
// once per second when Options.Console is set.
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
				// Drop streams idle for more than 30s so the assembler
				// does not accumulate dead connections forever.
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

func (s *Sniffer) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[sniffer] %s", msg)
	if s.opts.OnLog != nil {
		s.opts.OnLog(msg)
	}
}

func displayName(d pcap.Interface) string {
	if d.Description != "" {
		return d.Description
	}
	return d.Name
}

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
