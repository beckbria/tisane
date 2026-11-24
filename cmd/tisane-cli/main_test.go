package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tisane/pkg/profile"
	"tisane/pkg/search"
	"tisane/pkg/tsd"
)

const testBinDir = "/home/brian/tea/binary"

func requireCorpus(t testing.TB) {
	t.Helper()
	if _, err := os.Stat(testBinDir); os.IsNotExist(err) {
		t.Skip("binary test corpus not present")
	}
}

func TestLegacyTEACONSwitchesAndBatchStdin(t *testing.T) {
	requireCorpus(t)

	// Build a small accented/punctuated dictionary to verify /O:0..4 & /N:0|1 & /U:0|1 transforms.
	rawSrc := strings.Join([]string{
		"café-bar+ n.",
		"Œuvre+ n.",
		"Ice cream",
		"matador",
		"aorta",
	}, "\n") + "\n"
	tsdBytes, _, err := tsd.BuildBytes([]byte(rawSrc), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	tmpDir := t.TempDir()
	dictPath := filepath.Join(tmpDir, "accents.tsd")
	if err := os.WriteFile(dictPath, tsdBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. /O:4 (ASCII uppercase letters only), /N:0 (omit annotations), /Q (quiet), /D:<path>
	var stdout, stderr bytes.Buffer
	code := run(
		context.Background(),
		[]string{"/D:" + dictPath, "/O:4", "/N:0", "/Q", "cafe*", "œuvr*"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("legacy /O:4 run failed (%d): %s", code, stderr.String())
	}
	gotLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(gotLines) != 2 || gotLines[0] != "CAFEBAR" || gotLines[1] != "OEUVRE" {
		t.Fatalf("/O:4 output = %q, want [CAFEBAR OEUVRE]", gotLines)
	}

	// 2. /O:1 (ASCII preserving punctuation & case) with /N:1 (annotations included)
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{"/D:" + dictPath, "/O:1", "/N:1", "/Q", "cafe*"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 || strings.TrimSpace(stdout.String()) != "cafe-bar n." {
		t.Fatalf("/O:1 /N:1 output = %q (code=%d, err=%s)", stdout.String(), code, stderr.String())
	}

	// 3. Batch Stdin Mode with whitespace comment echoing & pattern headers (without /Q)
	stdinScript := "  === Batch Comment Header ===\n\tIndented Tab Comment\n.....*;aadmort\n"
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{"/D:" + dictPath, "/B:1"},
		strings.NewReader(stdinScript),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("stdin batch failed (%d): %s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"  === Batch Comment Header ===\n",
		"\tIndented Tab Comment\n",
		" .....*;aadmort\n",
		"aorta -DM\n",
		"matador\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("batch output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestFTSDefineStructuredFormatsAndEnumeration(t *testing.T) {
	requireCorpus(t)

	tsb2Path := filepath.Join(testBinDir, "tsbuild2.tsd")
	corePath := filepath.Join(testBinDir, "Dictionaries", "Core English.tsd")
	devilPath := filepath.Join(testBinDir, "Dictionaries", "The Devil's Dictionary.tsd")

	// 1. Full Text Search (-F) with set_difference ("crosswordman & !http") -> JSON output
	var stdout, stderr bytes.Buffer
	code := run(
		context.Background(),
		[]string{"-d", tsb2Path, "-F", "--format=json", "crosswordman & !http"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("-F --format=json exited %d: %s", code, stderr.String())
	}
	var jsonRecs []jsonMatchRecord
	if err := json.Unmarshal(stdout.Bytes(), &jsonRecs); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, stdout.String())
	}
	if len(jsonRecs) != 1 || jsonRecs[0].Word != "TEA Support" || !jsonRecs[0].HasDefinition {
		t.Fatalf("unexpected FTS JSON records: %+v", jsonRecs)
	}

	// 2. CSV output with --sort=smart, --dedupe, --show-dict across Core + Devil's Dictionary
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{
			"-d", corePath + ";" + devilPath,
			"--dedupe",
			"--sort=smart",
			"--format=csv",
			".....*;aadmort",
		},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("CSV search exited %d: %s", code, stderr.String())
	}
	rows, err := csv.NewReader(&stdout).ReadAll()
	if err != nil || len(rows) < 2 {
		t.Fatalf("CSV parse error: %v, rows=%v", err, rows)
	}
	if rows[0][0] != "word" || rows[1][0] != "matador" || rows[1][2] != "" {
		t.Fatalf("unexpected CSV header/top row: %v / %v", rows[0], rows[1])
	}

	// 3. --define on defined word ("tap") and undefined word ("xyzzynotaword")
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{"-d", tsb2Path, "--define", "tap", "xyzzynotaword"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("--define exited %d: %s", code, stderr.String())
	}
	defOut := stdout.String()
	if !strings.Contains(defOut, "tap (1 v.)") ||
		!strings.Contains(defOut, "tap (3 n.)") ||
		!strings.Contains(defOut, "https://www.onelook.com/?w=xyzzynotaword") {
		t.Fatalf("unexpected --define output:\n%s", defOut)
	}

	// 4. --enum shorthand ("9,3" -> matches "Crossword Man" in tsbuild2.tsd!)
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{"-d", tsb2Path, "-q", "--enum", "9,3", "*"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 || strings.TrimSpace(stdout.String()) != "Crossword Man" {
		t.Fatalf("--enum 9,3 output = %q (code=%d, err=%s)", stdout.String(), code, stderr.String())
	}

	// 5. Syntax error returns exit code 1 with diagnostic message & caret
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{"-d", tsb2Path, "[abc"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 1 || !strings.Contains(stderr.String(), "Pattern error: Choice list started - use ] to complete it") {
		t.Fatalf("expected exit code 1 & choice list error, got code=%d stderr=%q", code, stderr.String())
	}
}

func TestInteractiveREPLSession(t *testing.T) {
	requireCorpus(t)

	tsb2Path := filepath.Join(testBinDir, "tsbuild2.tsd")
	replInput := strings.Join([]string{
		":dicts",
		":val c.t",
		":val [bad1]",
		"aid | rich",
		":fts",
		"crosswordman & !http",
		":word",
		":enum 9,3",
		"*",
		":def rich",
		":q",
	}, "\n") + "\n"

	var stdout, stderr bytes.Buffer
	code := run(
		context.Background(),
		[]string{"-i", "-d", tsb2Path},
		strings.NewReader(replInput),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("REPL exited %d: stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"tsbuild2",
		"matches word lengths 3..3",
		"Choice lists can only include letters",
		"aid 📖",
		"TEA Support",
		"Crossword Man 📖",
		"rich adj. has a definition in Rich Text Format",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("REPL output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestCLIDiscoversExecutableDirAndDictionariesSubdir(t *testing.T) {
	exeDir := t.TempDir()
	dictsSubdir := filepath.Join(exeDir, "dictionaries")
	if err := os.MkdirAll(dictsSubdir, 0o755); err != nil {
		t.Fatal(err)
	}

	topBytes, _, err := tsd.BuildBytes([]byte("rootword\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	subBytes, _, err := tsd.BuildBytes([]byte("subword\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(exeDir, "top.tsd"), topBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dictsSubdir, "sub.tsd"), subBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	origExeFunc := profile.ExecutablePathFunc
	profile.ExecutablePathFunc = func() (string, error) {
		return filepath.Join(exeDir, "tisane-cli"), nil
	}
	defer func() {
		profile.ExecutablePathFunc = origExeFunc
	}()

	// Run from an empty working directory with an isolated empty config path and no -d flag.
	t.Chdir(t.TempDir())
	t.Setenv(profile.EnvConfigPath, filepath.Join(t.TempDir(), "config.toml"))

	var stdout, stderr bytes.Buffer
	code := run(
		context.Background(),
		[]string{"-q", "*word"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("CLI failed (%d): %s", code, stderr.String())
	}
	got := strings.Fields(stdout.String())
	if len(got) != 2 || got[0] != "rootword" || got[1] != "subword" {
		t.Fatalf("CLI output = %v, want [rootword subword]", got)
	}
}

func TestCLIHardeningConfigIsolationDefineJSONAndSignals(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(profile.EnvDictPath, "")
	t.Setenv("BUILD_WORKING_DIRECTORY", "")
	cfgPath := filepath.Join(tmp, "config.toml")
	t.Setenv(profile.EnvConfigPath, cfgPath)
	if err := profile.Save(cfgPath, profile.DefaultConfig()); err != nil {
		t.Fatal(err)
	}

	rawSrc := "alpha|First Greek letter\nbeta|Second Greek letter\n-dashword|Hyphenated word\n"
	tsdBytes, _, err := tsd.BuildBytes([]byte(rawSrc), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	dictPath := filepath.Join(tmp, "synth.tsd")
	if err := os.WriteFile(dictPath, tsdBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. REPL with auto-discovered dictionary must NOT pollute cfg.Dictionaries on exit,
	// while persisting RecentQueries cleanly.
	t.Chdir(tmp)
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"-i", "-q"}, strings.NewReader("alpha\n:q\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("REPL exited %d: %s", code, stderr.String())
	}
	reloaded, err := profile.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Dictionaries) != 0 {
		t.Fatalf("REPL polluted config.toml Dictionaries with runtime discovery: %+v", reloaded.Dictionaries)
	}
	if len(reloaded.RecentQueries) != 1 || reloaded.RecentQueries[0] != "alpha" {
		t.Fatalf("expected RecentQueries=[alpha], got %v", reloaded.RecentQueries)
	}

	// 2. Multi-word --define --format=json emits a single valid JSON array, and '--' allows words starting with '-'.
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{"-d", dictPath, "--define", "--format=json", "--", "alpha", "-dashword"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("multi --define JSON failed (%d): %s", code, stderr.String())
	}
	var defArr []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &defArr); err != nil || len(defArr) != 2 {
		t.Fatalf("expected valid 2-element JSON array from multi --define, err=%v raw=%s", err, stdout.String())
	}

	// 3. --count with --format=json and boolean =false flag (--dedupe=false).
	stdout.Reset()
	stderr.Reset()
	code = run(
		context.Background(),
		[]string{"-d", dictPath, "--dedupe=false", "--count", "--format=json", "*"},
		strings.NewReader(""),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("--count --format=json failed (%d): %s", code, stderr.String())
	}
	var cntRec jsonCountRecord
	if err := json.Unmarshal(stdout.Bytes(), &cntRec); err != nil || cntRec.Count != 3 {
		t.Fatalf("unexpected --count JSON: err=%v rec=%+v raw=%s", err, cntRec, stdout.String())
	}

	// 4. Pre-canceled context returns exit 130 without emitting truncated/empty JSON array.
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	stdout.Reset()
	stderr.Reset()
	code = run(canceledCtx, []string{"-d", dictPath, "--format=json", "*"}, strings.NewReader(""), &stdout, &stderr)
	if code != 130 || stdout.Len() != 0 {
		t.Fatalf("expected exit 130 and empty stdout on canceled context, got code=%d stdout=%q", code, stdout.String())
	}
}

func BenchmarkSearchCoreEnglish(b *testing.B) {
	requireCorpus(b)
	d, err := tsd.Open(filepath.Join(testBinDir, "Dictionaries", "Core English.tsd"))
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()

	eng := search.NewEngine()
	req := buildSearchRequest(".....*;aadmort", []*tsd.Dictionary{d}, nil, &cliOptions{
		AtMacro:      "[aeiou]",
		HashMacro:    "[^aeiou]",
		MinLen:       1,
		MaxLen:       100,
		ShowBalances: true,
		IgnorePunct:  true,
		EntryFilter:  search.EntryAll,
		Sort:         search.SortLenSmart,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, sErr := eng.Search(context.Background(), req); sErr != nil {
			b.Fatal(sErr)
		}
	}
}

func BenchmarkFixedTemplateEditedEnglish(b *testing.B) {
	requireCorpus(b)
	d, err := tsd.Open(filepath.Join(testBinDir, "Dictionaries", "Edited English.tsd"))
	if err != nil {
		b.Fatal(err)
	}
	defer d.Close()

	eng := search.NewEngine()
	req := search.DefaultRequest(".l...r.n", d)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, sErr := eng.Search(context.Background(), req); sErr != nil {
			b.Fatal(sErr)
		}
	}
}
