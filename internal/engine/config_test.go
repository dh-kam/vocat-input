package engine

import (
	"os"
	"path/filepath"
	"testing"

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
