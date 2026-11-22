//go:build windows

package main

import (
	"time"

	"golang.org/x/sys/windows"
)

// Códigos de tecla virtual usados por el atajo global Ctrl+Alt+M.
const (
	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkM       = 0x4D
)

var procGetAsyncKeyState = windows.NewLazySystemDLL("user32.dll").NewProc("GetAsyncKeyState")

func keyDown(vk uintptr) bool {
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return r&0x8000 != 0
}

// watchClickThroughHotkey sondea el teclado cada 50 ms y llama a toggle cuando se
// pulsa Ctrl+Alt+M. Se usa sondeo en lugar de RegisterHotKey porque funciona
// aunque el foco lo tenga el juego y no necesita un bucle de mensajes propio. Es
// la vía para salir del modo click-through, en el que la ventana no recibe
// clics.
func watchClickThroughHotkey(stop <-chan struct{}, toggle func()) {
	if procGetAsyncKeyState.Find() != nil {
		return
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	pressed := false
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			down := keyDown(vkControl) && keyDown(vkMenu) && keyDown(vkM)
			if down && !pressed {
				toggle()
			}
			pressed = down
		}
	}
}
