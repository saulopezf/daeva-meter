//go:build !windows

package main

// watchClickThroughHotkey no tiene implementación fuera de Windows; el modo
// click-through se alterna solo desde la bandeja del sistema.
func watchClickThroughHotkey(stop <-chan struct{}, _ func()) {
	<-stop
}
