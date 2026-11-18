package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func envKey(name string) (string, error) {
	if value := os.Getenv(name); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("%s is not set (pass -env .env.development, run from services/go-api)", name)
}

func loadEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		if err := setFromLine(lines.Text()); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return lines.Err()
}

func setFromLine(line string) error {
	name, value, isSetting := strings.Cut(strings.TrimSpace(line), "=")
	if !isSetting || name == "" || strings.HasPrefix(name, "#") || os.Getenv(name) != "" {
		return nil
	}
	return os.Setenv(name, unquote(value))
}

func unquote(value string) string {
	for _, quote := range []string{`"`, `'`} {
		if len(value) >= 2 && strings.HasPrefix(value, quote) && strings.HasSuffix(value, quote) {
			return value[1 : len(value)-1]
		}
	}
	return value
}
