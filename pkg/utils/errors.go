package utils

import "fmt"

// Returns a new error which contains some contextual error message along with the original error
// message.
func WrapError(contextualErrorMessage string, err error) error {
	return fmt.Errorf("%s : %s", contextualErrorMessage, err)
}
