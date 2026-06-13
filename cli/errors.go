package cli

import (
	"errors"

	"github.com/tamnd/craftinginterpreters-cli/craftinginterpreters"
)

func isNotFound(err error) bool {
	return errors.Is(err, craftinginterpreters.ErrNotFound)
}
