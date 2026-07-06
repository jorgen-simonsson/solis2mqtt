package config

import (
	"bufio"
	"os"
	"strings"
)

// LoadDotEnv reads simple KEY=VALUE lines from path and applies them via
// os.Setenv, without overriding variables already set in the process
// environment (so real env vars, e.g. injected by docker-compose, always
// take precedence over the file). Missing files are not an error, since
// .env is optional when variables are supplied another way.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, value)
	}
	return scanner.Err()
}
