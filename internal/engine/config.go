package engine

import (
	"fmt"
	"os"
	"regexp"
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

// secretsInURLs matches the credential shapes this project puts into request URLs: the Google API
// key that getGoogleOCRAuth appends as a query parameter, and the Telegram bot token that sits in
// the URL path.
var secretsInURLs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)([?&](?:key|api[-_]?key|access[-_]?token)=)[^&\s"'\\]+`),
	regexp.MustCompile(`(/bot)[0-9]+:[A-Za-z0-9_-]+`),
}

// RedactSecrets strips credentials out of a message before it is logged or stored.
//
// net/http returns *url.Error carrying the full request URL, and Go's own redaction covers only
// userinfo passwords - not query parameters. So any transport failure on a Vertex call built with
// "?key=..." put the API key straight into the error text, which the server appends to run.Logs,
// persists in storage/runs_db.json and renders in the web UI. The Telegram token leaked the same
// way through its URL path.
func RedactSecrets(msg string) string {
	for _, re := range secretsInURLs {
		msg = re.ReplaceAllString(msg, "${1}REDACTED")
	}
	return msg
}

// RedactedError wraps err with a message whose credentials have been removed. It returns a plain
// error rather than preserving the chain, because the original text is exactly what must not
// survive.
func RedactedError(format string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format, RedactSecrets(err.Error()))
}
