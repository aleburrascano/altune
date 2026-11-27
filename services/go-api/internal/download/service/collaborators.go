package service

import (
	"errors"
	"fmt"
	"reflect"
)

var ErrMissingCollaborator = errors.New("a required collaborator is missing")

func missingPort[P any](owner, name string, port P) error {
	if isNil(port) {
		return errMissingPort(owner, name)
	}
	return nil
}

func isNil[P any](port P) bool {
	switch reflect.TypeFor[P]().Kind() {
	case reflect.Interface:
		return any(port) == nil
	case reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.Chan:
		return reflect.ValueOf(port).IsNil()
	}
	return false
}

func errMissingPort(owner, name string) error {
	return fmt.Errorf("%s: %s: %w; pass an explicit adapter when the wiring has none", owner, name, ErrMissingCollaborator)
}
