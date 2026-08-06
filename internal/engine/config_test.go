package engine

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withEnvFile runs fn with the process working directory pointed at a temp dir containing the
// given .env contents, so LookupConfig's cwd-relative read is exercised for real.
func withEnvFile(t *testing.T, contents string, fn func()) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, envFileName), []byte(contents), 0o600))

	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	fn()
}

// The bug this helper exists to kill: a value containing '=' used to be cut at the second one.
func TestLookupConfig_KeepsValuesContainingEquals(t *testing.T) {
	cases := map[string]string{
		"base64 padding":     "dGVzdHZhbHVl==",
		"single trailing eq": "ABSKQmVkcm9ja0FQSUtlefg=",
		"embedded equals":    "a=b=c",
		"no equals":          "plainvalue",
	}

	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			withEnvFile(t, "AWS_BEARER_TOKEN_BEDROCK="+want+"\n", func() {
				assert.Equal(t, want, LookupConfig("AWS_BEARER_TOKEN_BEDROCK"))
			})
		})
	}
}

func TestLookupConfig_EnvironmentWinsOverFile(t *testing.T) {
	withEnvFile(t, "VOCAT_TEST_KEY=from-file\n", func() {
		t.Setenv("VOCAT_TEST_KEY", "from-env")
		assert.Equal(t, "from-env", LookupConfig("VOCAT_TEST_KEY"))
	})
}

func TestLookupConfig_FallsBackToFile(t *testing.T) {
	withEnvFile(t, "VOCAT_TEST_KEY=from-file\n", func() {
		assert.Equal(t, "from-file", LookupConfig("VOCAT_TEST_KEY"))
	})
}

// Several call sites accept a list of aliases; the first key with a value must win, in the
// order the caller listed them, not the order the file happens to use.
func TestLookupConfig_FirstKeyWithAValueWins(t *testing.T) {
	withEnvFile(t, "GEMINI_API_KEY=gemini\nVERTEX_API_KEY=vertex\n", func() {
		assert.Equal(t, "vertex", LookupConfig("VERTEX_API_KEY", "GEMINI_API_KEY"))
		assert.Equal(t, "gemini", LookupConfig("GEMINI_API_KEY", "VERTEX_API_KEY"))
	})
}

func TestLookupConfig_IgnoresCommentsBlanksAndPrefixes(t *testing.T) {
	contents := "" +
		"# VOCAT_TEST_KEY=commented-out\n" +
		"\n" +
		"   \n" +
		"NOT_AN_ASSIGNMENT\n" +
		"export VOCAT_TEST_KEY=exported\n"

	withEnvFile(t, contents, func() {
		assert.Equal(t, "exported", LookupConfig("VOCAT_TEST_KEY"))
	})
}

func TestLookupConfig_StripsSurroundingQuotes(t *testing.T) {
	withEnvFile(t, "A=\"double\"\nB='single'\nC=\"unbalanced\n", func() {
		assert.Equal(t, "double", LookupConfig("A"))
		assert.Equal(t, "single", LookupConfig("B"))
		assert.Equal(t, "\"unbalanced", LookupConfig("C"), "an unbalanced quote is part of the value")
	})
}

// A key whose prefix matches another must not be confused with it — the old HasPrefix checks
// would accept VOCAT_SESSION_SECRET_OLD when asked for VOCAT_SESSION_SECRET.
func TestLookupConfig_MatchesWholeKeyOnly(t *testing.T) {
	withEnvFile(t, "VOCAT_SESSION_SECRET_OLD=stale\n", func() {
		assert.Empty(t, LookupConfig("VOCAT_SESSION_SECRET"))
	})
}

func TestLookupConfig_MissingKeyAndMissingFile(t *testing.T) {
	withEnvFile(t, "OTHER=value\n", func() {
		assert.Empty(t, LookupConfig("VOCAT_TEST_KEY"))
	})

	dir := t.TempDir() // no .env at all
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	assert.Empty(t, LookupConfig("VOCAT_TEST_KEY"))
}

// The exact leak shape: net/http returns *url.Error containing the whole URL, and Go redacts only
// userinfo passwords, so a Vertex endpoint built with "?key=..." carried the API key into run
// logs, storage/runs_db.json and the web UI.
func TestRedactSecrets_URLCredentials(t *testing.T) {
	cases := []struct {
		name, in, mustNotContain string
	}{
		{
			name:           "vertex api key in the query",
			in:             `Post "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models/gemini-2.5-flash:generateContent?key=AQ.SECRET_KEY_VALUE": context deadline exceeded`,
			mustNotContain: "AQ.SECRET_KEY_VALUE",
		},
		{
			name:           "telegram bot token in the path",
			in:             `Post "https://api.telegram.org/bot123456789:AAHxSECRETtokenVALUE_-x/sendDocument": dial tcp: lookup failed`,
			mustNotContain: "AAHxSECRETtokenVALUE_-x",
		},
		{
			name:           "api_key spelling",
			in:             `Get "https://example.com/v1?api_key=SECRET123&x=1": tls handshake timeout`,
			mustNotContain: "SECRET123",
		},
		{
			name:           "access_token spelling",
			in:             `Get "https://example.com/v1?foo=1&access_token=SECRET456": EOF`,
			mustNotContain: "SECRET456",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSecrets(tc.in)
			assert.NotContains(t, got, tc.mustNotContain, "credential survived redaction: %s", got)
			assert.Contains(t, got, "REDACTED")
			// The diagnostic value has to survive, or the redaction just hides the failure.
			assert.Contains(t, got, "https://")
		})
	}
}

func TestRedactSecrets_LeavesHarmlessTextAlone(t *testing.T) {
	for _, msg := range []string{
		"bedrock request failed for amazon.nova-lite-v1:0: connection reset",
		`unmarshal vertex response: invalid character 'x'`,
		"AWS_BEARER_TOKEN_BEDROCK not set in environment or .env",
		"",
	} {
		assert.Equal(t, msg, RedactSecrets(msg))
	}
}

// A key parameter must not swallow the rest of the query string.
func TestRedactSecrets_StopsAtTheParameterBoundary(t *testing.T) {
	got := RedactSecrets(`Get "https://example.com/v1?key=SECRET&alt=json&pretty=true": EOF`)
	assert.NotContains(t, got, "SECRET")
	assert.Contains(t, got, "alt=json")
	assert.Contains(t, got, "pretty=true")
}

func TestRedactedError(t *testing.T) {
	assert.NoError(t, RedactedError("wrapped: %s", nil))

	err := RedactedError("vertex api request failed: %s",
		fmt.Errorf(`Post "https://x.googleapis.com/v1:generateContent?key=AQ.LEAKED": timeout`))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "AQ.LEAKED")
	assert.Contains(t, err.Error(), "vertex api request failed")
	assert.Contains(t, err.Error(), "timeout")
}

// Builds a genuine *url.Error rather than a hand-written string, to confirm what net/http actually
// puts in the message and that redaction covers it. Go's own stripPassword only touches userinfo,
// so the query parameter survives untouched without RedactSecrets.
func TestRedactSecrets_AgainstARealTransportError(t *testing.T) {
	const canary = "AQ.CANARY_SECRET_abcdef123456"

	// .invalid never resolves (RFC 2606), so this fails in the transport layer.
	url := "https://vocat-nonexistent.invalid/v1/models/gemini:generateContent?key=" + canary
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	require.NoError(t, err)

	client := &http.Client{Timeout: 5 * time.Second}
	_, doErr := client.Do(req)
	require.Error(t, doErr, "the request must fail for this test to mean anything")

	raw := doErr.Error()
	require.Contains(t, raw, canary, "precondition: net/http does leak the query parameter")

	redacted := RedactSecrets(raw)
	assert.NotContains(t, redacted, canary, "credential survived: %s", redacted)
	assert.Contains(t, redacted, "key=REDACTED")
	assert.Contains(t, redacted, "vocat-nonexistent.invalid", "the host must remain for diagnosis")
}
