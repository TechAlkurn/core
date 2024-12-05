package lib

import "errors"

func Throw(message string) error {
	return errors.New(message)
}
