//go:build vendoring
// +build vendoring

package vendoring

// packages to forcibly add to vendoring because they are needed during (container) builds

import (
	_ "github.com/google/go-licenses"
)
