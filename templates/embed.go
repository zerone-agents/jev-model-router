package templates

import _ "embed"

//go:embed balanced.md
var balanced string

func Balanced() string { return balanced }
