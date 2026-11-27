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
	kind := reflect.TypeFor[P]().Kind()
	if kind == reflect.Interface {
		return any(port) == nil
	}
	nilable := kind == reflect.Func || kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice || kind == reflect.Chan
	return nilable && reflect.ValueOf(port).IsNil()
}

func errMissingPort(owner, name string) error {
	return fmt.Errorf("%s: %s: %w; pass an explicit adapter when the wiring has none", owner, name, ErrMissingCollaborator)
}
