//go:build dev

package main

// sessionLogs habilita el log de paquetes por sesión. `wails dev` compila la app
// con el tag "dev", así que el log solo se escribe en desarrollo y nunca en una
// compilación de producción hecha con `wails build`.
const sessionLogs = true
