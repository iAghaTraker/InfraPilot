package web

import "embed"

// Assets contains the static Web Panel UI embedded in the web binary.
//
//go:embed assets/index.html assets/app.css assets/app.js
var Assets embed.FS
