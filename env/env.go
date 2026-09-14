package env

import (
	"bufio"
	"log"
	"os"
	"strings"
)

// Credentials and endpoints loaded from real environment variables first,
// falling back to the .env file at the project root.
var (
	GITHUB_CLIENT_ID     string
	GITHUB_CLIENT_SECRET string
	GITHUB_CALLBACK_URL  string
	DISCORD_WEBHOOK_URL  string
)

func init() {
	fileVars := readEnvFile(PATH + ".env")

	GITHUB_CLIENT_ID = lookup("GITHUB_CLIENT_ID", fileVars)
	GITHUB_CLIENT_SECRET = lookup("GITHUB_CLIENT_SECRET", fileVars)
	GITHUB_CALLBACK_URL = lookup("GITHUB_CALLBACK_URL", fileVars)
	DISCORD_WEBHOOK_URL = lookup("DISCORD_WEBHOOK_URL", fileVars)
}

// lookup returns the value of key from the process environment, falling back
// to the values parsed from the .env file.
func lookup(key string, fileVars map[string]string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fileVars[key]
}

// readEnvFile parses a .env file of KEY=VALUE lines, ignoring blank lines and
// comments. A missing file is not an error: values may come from the process
// environment instead.
func readEnvFile(path string) map[string]string {
	vars := make(map[string]string)

	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error reading .env file: %v\n", err)
		}
		return vars
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		vars[strings.TrimSpace(key)] = value
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error scanning .env file: %v\n", err)
	}

	return vars
}
