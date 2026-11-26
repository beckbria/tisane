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

func isolateCLITestEnv(t *testing.T) {
	t.Helper()
	tmpCfg := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv(profile.EnvConfigPath, tmpCfg)
	t.Setenv(profile.EnvDictPath, "")
	t.Setenv("BUILD_WORKING_DIRECTORY", "")
}

func buildCLIFixtures(t *testing.T) (tsb2Path, corePath, devilPath string) {
	t.Helper()
	dir := t.TempDir()

	tsb2Src := strings.Join([]string{
		`aid|assistance and support given to someone`,
		`rich|{\rtf1 \b rich\b0  \i adj.\i0  has a definition in Rich Text Format}`,
		`tap++ 1 v.|first definition for tap as verb`,
		`tap++ 3 n.|third definition for tap as noun`,
		`Crossword Man|creator of TEA at http://www.crosswordman.com`,
		`TEA Support|contact crosswordman for dictionary format help`,
		`spoon|utensil with a shallow bowl`,
		`plate|flat dish from which food is served`,
	}, "\n") + "\n"
	tsb2Opts := tsd.DefaultBuildOptions()
	tsb2Opts.MaxFTSPercent = 50
	tsb2Bytes, _, err := tsd.BuildBytes([]byte(tsb2Src), tsb2Opts)
	if err != nil {
		t.Fatal(err)
	}
	tsb2Path = filepath.Join(dir, "tsbuild2.tsd")
	if err := os.WriteFile(tsb2Path, tsb2Bytes, 0o644); err != nil {
		t.Fatal(err)
	}

	coreBytes, _, err := tsd.BuildBytes([]byte("aorta\ndrama\nmatador\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	corePath = filepath.Join(dir, "Core English.tsd")
	if err := os.WriteFile(corePath, coreBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	devilBytes, _, err := tsd.BuildBytes([]byte("matador|bullfighter\ncynic|faulty vision\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	devilPath = filepath.Join(dir, "The Devil's Dictionary.tsd")
	if err := os.WriteFile(devilPath, devilBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	return tsb2Path, corePath, devilPath
}

func TestLegacyTEACONSwitchesAndBatchStdin(t *testing.T) {
	isolateCLITestEnv(t)

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
	isolateCLITestEnv(t)
	tsb2Path, corePath, devilPath := buildCLIFixtures(t)

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

	// 4. --enum shorthand ("9,3" -> matches "Crossword Man")
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
	isolateCLITestEnv(t)
	tsb2Path, _, _ := buildCLIFixtures(t)

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
	isolateCLITestEnv(t)

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

	t.Chdir(t.TempDir())

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

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	stdout.Reset()
	stderr.Reset()
	code = run(canceledCtx, []string{"-d", dictPath, "--format=json", "*"}, strings.NewReader(""), &stdout, &stderr)
	if code != 130 || stdout.Len() != 0 {
		t.Fatalf("expected exit 130 and empty stdout on canceled context, got code=%d stdout=%q", code, stdout.String())
	}

	// Verify REPL :len alias and :fts :val ignoring word :len/:enum bounds.
	stdout.Reset()
	stderr.Reset()
	replCommands := ":len 7 7\n:enum 4,3\n:fts\n:val blackguard\n:q\n"
	code = run(context.Background(), []string{"-d", dictPath, "-i", "-q"}, strings.NewReader(replCommands), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("REPL :fts/:val/:len failed (%d): %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Length range set to 7..7.") || !strings.Contains(stdout.String(), "Valid (") {
		t.Fatalf("unexpected REPL output for :len and :fts :val:\nstdout=%s\nstderr=%s", stdout.String(), stderr.String())
	}

	// Verify /O:4 transliterates Windows-1252 Ž/ž (0x8E/0x9E) to Z/z.
	if got := transformWordAndAnnot("Žižek", "", false, 4); got != "ZIZEK" {
		t.Fatalf("transformWordAndAnnot(Žižek, mode=4) = %q, want %q", got, "ZIZEK")
	}
}

func TestCLIMigratesObsoleteWordNetWithoutDuplicateResults(t *testing.T) {
	tmpDir := t.TempDir()
	dictsDir := filepath.Join(tmpDir, "dictionaries")
	if err := os.MkdirAll(dictsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(profile.EnvDictPath, dictsDir)
	t.Setenv(profile.EnvConfigPath, filepath.Join(tmpDir, "config.toml"))
	t.Setenv("BUILD_WORKING_DIRECTORY", "")
	t.Chdir(tmpDir)

	origExe := profile.ExecutablePathFunc
	profile.ExecutablePathFunc = func() (string, error) {
		return filepath.Join(tmpDir, "tisane-cli"), nil
	}
	defer func() {
		profile.ExecutablePathFunc = origExe
	}()

	raw, _, err := tsd.BuildBytes([]byte("quartz\n"), tsd.DefaultBuildOptions())
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dictsDir, "english-wordnet-2025-plus.tsd")
	newPath := filepath.Join(dictsDir, "English Wordnet 2025 Plus.tsd")
	if err := os.WriteFile(oldPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	// Pass --no-dedupe so duplicate open dictionaries would produce 2 hits if migration hadn't cleaned up oldPath.
	code := run(context.Background(), []string{"--no-dedupe", "-q", "quartz"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("tisane-cli exited %d: %s", code, stderr.String())
	}
	lines := strings.Fields(stdout.String())
	if len(lines) != 1 || lines[0] != "quartz" {
		t.Fatalf("expected exactly 1 match after obsolete-file cleanup, got %v (stderr=%s)", lines, stderr.String())
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected obsolete %q deleted, stat err = %v", oldPath, err)
	}
}

func BenchmarkSearchWordNet(b *testing.B) {
	wordnetPath := filepath.Join("..", "..", "dictionaries", "English Wordnet 2025 Plus.tsd")
	d, err := tsd.Open(wordnetPath)
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

func BenchmarkFixedTemplateWordNet(b *testing.B) {
	wordnetPath := filepath.Join("..", "..", "dictionaries", "English Wordnet 2025 Plus.tsd")
	d, err := tsd.Open(wordnetPath)
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
