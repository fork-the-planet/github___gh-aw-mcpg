package config

import (
	"sync"

	"github.com/github/gh-aw-mcpg/internal/logger"
	"github.com/itchyny/gojq"
)

var logValidation = logger.New("config:validation")

// secureCompileOpts are the gojq compiler options applied to every Compile call in this
// package. Centralising them here ensures the security intent ($ENV disabled) is never
// accidentally omitted from a future compile site.
var secureCompileOpts = []gojq.CompilerOption{
	gojq.WithEnvironLoader(func() []string { return nil }), // explicitly disable $ENV access (defense-in-depth)
}

// customSchemaCache stores compiled custom schemas by schema URL to avoid
// repeated fetch + compile work across validations.
var customSchemaCache sync.Map
