package web

import "embed"

// Assets contains the static Web Panel UI embedded in the web binary.
//
//go:embed dist/index.html dist/assets/*
var Assets embed.FS
