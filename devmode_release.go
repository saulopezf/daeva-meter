//go:build production

package main

// devMode está desactivado en compilaciones de producción: la interfaz no
// muestra el panel de diagnóstico de desarrollo.
const devMode = false
