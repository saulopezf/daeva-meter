//go:build production

package main

// sessionLogs está desactivado en compilaciones de producción: no se crea
// ningún archivo de log de sesión.
const sessionLogs = false
