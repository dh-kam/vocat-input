package engine

import (
	"os"
	"strings"
)

// envFileName is the config file consulted when a value is absent from the process
// environment. The module has no dotenv dependency and nothing exports this file into the
// environment before startup, so the fallback README documents is implemented here.
const envFileName = ".env"

// LookupConfig returns the first non-empty value among keys, checking the process environment
// before falling back to ./.env.
//
// Values are split on the first '=' only. Splitting on every '=' — which each of the call sites
// used to do independently — silently truncated any value containing one, and base64 secrets
// routinely end in '=' padding: the AWS_BEARER_TOKEN_BEDROCK currently in this repo's .env is
// 132 characters and lost its last one.
func LookupConfig(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}

	// Both paths resolve in the order the caller listed its keys, since that order expresses
	// preference between aliases. Reading the file into a map first keeps that true regardless
	// of the order the keys happen to appear in the file.
	values := readEnvFile()
	for _, k := range keys {
		if v := values[k]; v != "" {
			return v
		}
	}
	return ""
}

// readEnvFile parses ./.env into key/value pairs. A key repeated in the file keeps its first
// value; a missing or unreadable file is not an error, it just yields nothing.
func readEnvFile() map[string]string {
	values := map[string]string{}
	data, err := os.ReadFile(envFileName)
	if err != nil {
		return values
	}
	for line := range strings.Lines(string(data)) {
		key, value, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if _, seen := values[key]; !seen {
			values[key] = value
		}
	}
	return values
}

// parseEnvLine splits one .env line into its key and value, tolerating the surface syntax such
// files normally carry: leading `export `, surrounding quotes, comments and blank lines. The
// first match in the file wins.
func parseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")

	key, value, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, key != ""
}
