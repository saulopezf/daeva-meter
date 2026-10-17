//go:build dev

package main

// sessionLogs enables the per-session packet log. `wails dev` compiles the app
// with the "dev" build tag, so the log is only written during development and
// never in a `wails build` release.
const sessionLogs = true
