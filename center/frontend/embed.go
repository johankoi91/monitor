// Package frontend embeds the React build served by the center.
package frontend

import "embed"

//go:embed static/*
var Files embed.FS
