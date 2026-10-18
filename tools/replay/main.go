// Command replay pasa un log de captura del sniffer por el parser de aion2 e
// imprime las entidades resueltas y la tabla de DPS. Permite desarrollar y
// verificar la capa de protocolo sin el juego ni Npcap en marcha.
//
//	go run ./tools/replay captures/session-20261016-215243.txt
package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"daeva-meter/aion2"
)

// lineRe reconoce cada línea del log de sesión escrita por app.writePayload:
//
//	+<segundos> <S2C|C2S> stream=<id> len=<n> <hex>
var lineRe = regexp.MustCompile(`^\+([0-9.]+) (S2C|C2S) stream=(\d+) len=(\d+) ([0-9a-f]+)$`)

func main() {
	flag.Parse()
	path := flag.Arg(0)
	if path == "" {
		path = "captures/session-20261016-215243.txt"
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	combat := aion2.NewCombat()
	parser := aion2.NewParser(combat)
	// Los tiempos del log son relativos, así que se usa una base fija.
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var maxSec float64
	var lines, fed int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	for sc.Scan() {
		lines++
		m := lineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		sec, _ := strconv.ParseFloat(m[1], 64)
		if sec > maxSec {
			maxSec = sec
		}
		if m[2] != "S2C" {
			continue // solo interesa el sentido servidor->cliente
		}
		stream, _ := strconv.ParseInt(m[3], 10, 64)
		data, err := hex.DecodeString(m[5])
		if err != nil {
			continue
		}
		ts := base.Add(time.Duration(sec * float64(time.Second)))
		parser.Feed(stream, data, ts)
		fed++
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "scan:", err)
		os.Exit(1)
	}

	now := base.Add(time.Duration(maxSec * float64(time.Second)))
	snap := combat.Snapshot(now)

	fmt.Printf("file: %s (%d lines, %d S2C segments, %.1fs)\n", path, lines, fed, maxSec)
	fmt.Printf("self: #%d %q\n", snap.SelfID, snap.Self)
	fmt.Printf("party: %v\n", snap.Party)
	fmt.Println("\n-- entities --")
	for _, e := range combat.Entities() {
		kind := "?"
		switch {
		case e.Self:
			kind = "SELF"
		case e.IsPlayer:
			kind = "PLAYER"
		case e.NPC:
			kind = "NPC"
		}
		cls, _ := aion2.ClassName(e.Class)
		owner := ""
		if e.Owner > 0 {
			owner = fmt.Sprintf(" owner=#%d", e.Owner)
		}
		fmt.Printf("  #%-8d %-7s %-12s lvl=%-3d %s%s\n", e.ID, kind, cls, e.Level, e.Name, owner)
	}

	fmt.Printf("\n-- DPS board (elapsed %.1fs, active=%v) --\n", snap.Elapsed, snap.Active)
	fmt.Printf("  %-14s %-12s %10s %10s %6s %6s\n", "name", "class", "damage", "dps", "hits", "crits")
	for _, m := range snap.Members {
		fmt.Printf("  %-14s %-12s %10d %10.0f %6d %6d\n", m.Name, m.Class, m.Damage, m.DPS, m.Hits, m.Crits)
	}
}
