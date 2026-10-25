//go:build !production

package main

// sessionLogs habilita el log de paquetes por sesión en compilaciones de
// desarrollo. Wails v3 compila en modo desarrollo sin el tag "production" y en
// producción con `-tags production`, así que el log nunca se escribe en una
// compilación de release.
const sessionLogs = true
