package config

import (
	"sync"

	"github.com/github/gh-aw-mcpg/internal/jqutil"
	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logValidation = logger.New("config:validation")

// secureCompileOpts delegates to jqutil.SecureCompileOpts so that all packages
// share a single authoritative definition of the $ENV-disabled gojq compile options.
var secureCompileOpts = jqutil.SecureCompileOpts

// customSchemaCache stores compiled custom schemas by schema URL to avoid
// repeated fetch + compile work across validations.
var customSchemaCache sync.Map
