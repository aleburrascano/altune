package decodedfields

import (
	"altune/go-api/internal/shared/providerfields/internal/core/schema"
	"fmt"
	"strings"
)

const (
	readByKeys = ""
	bodyRoot   = ""
)

func Read(dirs []string, typeName string) (schema.Reads, error) {
	pkg, err := readPackage(dirs)
	if err != nil {
		return schema.Reads{}, err
	}
	if typeName == readByKeys {
		return schema.Reads{Paths: map[string]bool{}, Opaque: []string{bodyRoot}, Keys: pkg.lookedUpKeys()}, nil
	}
	root, typeArgs, err := pkg.decodeStruct(typeName)
	if err != nil {
		return schema.Reads{}, fmt.Errorf("%w in %v", err, dirs)
	}
	return walk(pkg, root, typeArgs), nil
}

func (p *goPackage) decodeStruct(typeName string) (*declaredStruct, map[string]string, error) {
	name, args := splitTypeArgs(typeName)
	declared, ok := p.structs[name]
	if !ok || declared.declarations > 1 {
		return nil, nil, fmt.Errorf("no single struct %s", name)
	}
	if len(declared.typeParams) != len(args) {
		return nil, nil, fmt.Errorf("%s takes type arguments %v, got %v", name, declared.typeParams, args)
	}
	typeArgs := map[string]string{}
	for i, param := range declared.typeParams {
		typeArgs[param] = args[i]
	}
	return declared, typeArgs, nil
}

func splitTypeArgs(typeName string) (string, []string) {
	name, rest, generic := strings.Cut(typeName, "[")
	if !generic {
		return name, nil
	}
	args := strings.Split(strings.TrimSuffix(rest, "]"), ",")
	for i := range args {
		args[i] = strings.TrimSpace(args[i])
	}
	return name, args
}
