package web

import "embed"

// Dist holds the built Vue application. It is compiled into the binary, so
// starting the server is all it takes to get the UI: no npm, no second process,
// no CORS, no separate port.
//
//go:embed all:dist
var Dist embed.FS
