//go:build !dev

package main

// sessionLogs is off in production builds: no session log file is created.
const sessionLogs = false
