package main

import "embed"

//go:embed templates static
var staticFiles embed.FS
