//go:build !production

package main

// devMode indica que la aplicación se ha compilado en modo desarrollo (sin el
// tag "production"). Se usa para mostrar en la interfaz el panel de diagnóstico
// (estado de captura, puerto, log de sesión y contadores de red), que no debe
// existir en la aplicación distribuida.
const devMode = true
