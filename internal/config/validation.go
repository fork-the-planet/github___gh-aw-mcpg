package config

import (
	"sync"

	"github.com/github/gh-aw-mcpg/internal/logger"
)

var logValidation = logger.New("config:validation")

// customSchemaCache stores compiled custom schemas by schema URL to avoid
// repeated fetch + compile work across validations.
var customSchemaCache sync.Map
