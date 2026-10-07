package inference

import (
	"errors"
	"github.com/zerone-agents/jev-model-router/internal/routing"
)

func routingErrorCode(err error) string {
	var known *routing.Error
	if errors.As(err, &known) {
		return known.Code
	}
	return "internal_error"
}
