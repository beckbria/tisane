package tsd_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"unicode/utf8"

	"tisane/pkg/tsd"
)

var dictionariesDir = filepath.Join("..", "..", "dictionaries")

func requireDictionariesDir(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(dictionariesDir); os.IsNotExist(err) {
		t.Skipf("dictionaries directory not present at %s", dictionariesDir)
	}
}

func TestCP1252RoundTrip(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := 0; i < 256; i++ {
		allBytes[i] = byte(i)
		r := tsd.DecodeCP1252Byte(byte(i))
		gotB, ok := tsd.EncodeCP1252Rune(r)
		if !ok || gotB != byte(i) {
			t.Fatalf("byte 0x%02X -> rune U+%04X -> (0x%02X, %v)", i, r, gotB, ok)
		}
	}

	s := tsd.DecodeCP1252(allBytes)
	if !utf8.ValidString(s) {
		t.Fatalf("DecodeCP1252 produced invalid UTF-8")
	}
	roundTrip, ok := tsd.EncodeCP1252(s)
	if !ok || !bytes.Equal(roundTrip, allBytes) {
		t.Fatalf("EncodeCP1252(DecodeCP1252(0..255)) mismatch (ok=%v)", ok)
	}

	// Verify unrepresentable Unicode reports ok == false.
	lossy, ok := tsd.EncodeCP1252("hello ☃ world")
	if ok {
		t.Fatalf("expected ok=false for snowman rune")
	}
	if string(lossy) != "hello ? world" {
		t.Fatalf("unexpected lossy output: %q", lossy)
	}
}

func TestLoadTSMFiles(t *testing.T) {
	defaultLoaded, err := tsd.LoadTSM(filepath.Join("testdata", "default.tsm"))
	if err != nil {
		t.Fatalf("LoadTSM(default.tsm): %v", err)
	}
	if defaultLoaded != tsd.DefaultTSM() {
		t.Fatalf("default.tsm does not match built-in DefaultTSM()")
	}
	if got := defaultLoaded.NumClasses(); got != 63 {
		t.Errorf("default.tsm: NumClasses()=%d, want 63", got)
	}
	if defaultLoaded.IsAlpha(0x00) || defaultLoaded.IsAlpha(0x0A) || defaultLoaded.IsAlpha(0x0D) {
		t.Errorf("default.tsm: mandatory non-alphabetic control byte marked alphabetic")
	}

	customLoaded, err := tsd.LoadTSM(filepath.Join("testdata", "custom.tsm"))
	if err != nil {
		t.Fatalf("LoadTSM(custom.tsm): %v", err)
	}
	if got := customLoaded.NumClasses(); got != 26 {
		t.Errorf("custom.tsm: NumClasses()=%d, want 26", got)
	}
	// Verify lowercase folding and transitive closure (0xE9 'é' -> 'e' -> 'E').
	if got := customLoaded.Mapped('a'); got != 'A' {
		t.Errorf("custom.tsm: Mapped('a')=%q, want 'A'", got)
	}
	if got := customLoaded.Mapped(0xE9); got != 'E' {
		t.Errorf("custom.tsm: Mapped(0xE9)=%q, want 'E'", got)
	}
	if customLoaded.IsAlpha(' ') || customLoaded.IsAlpha('0') || customLoaded.IsAlpha(0x00) || customLoaded.IsAlpha(0x0A) || customLoaded.IsAlpha(0x0D) {
		t.Errorf("custom.tsm: expected punctuation/digits/control bytes to be non-alphabetic")
	}

	// Verify malformed .tsm input returns an error.
	if _, err := tsd.ParseTSM([]byte("invalid_line\n")); err == nil {
		t.Errorf("ParseTSM: expected error for malformed line")
	}
}

func TestTSDWalkthrough(t *testing.T) {
	d, err := tsd.Open(filepath.Join("testdata", "walkthrough.tsd"))
	if err != nil {
		t.Fatalf("Open(walkthrough.tsd): %v", err)
	}
	defer d.Close()

	if d.Version() != tsd.VersionTSD1 {
		t.Fatalf("Version() = %s, want TSD1", d.Version())
	}
	if d.Description() != "Tisane Walkthrough Test Fixture" {
		t.Fatalf("Description() = %q", d.Description())
	}
	if d.TotalWords() != 18 || d.MaxWordLength() != 12 {
		t.Fatalf("TotalWords=%d MaxWordLength=%d, want 18 and 12", d.TotalWords(), d.MaxWordLength())
	}
	if !d.HasDefinitions() || !d.HasPass2Definitions() || !d.HasFTSIndex() {
		t.Fatalf("expected definitions and FTS index to be reported present")
	}

	// Check L=3 dictionary words ("aid", "tap").
	var words3 []tsd.WordEntry
	for w, err := range d.ScanWords(3) {
		if err != nil {
			t.Fatalf("ScanWords(3): %v", err)
		}
		words3 = append(words3, w)
	}
	if len(words3) != 2 || words3[0].Word != "aid" || words3[1].Word != "tap" {
		t.Fatalf("unexpected L=3 words: %+v", words3)
	}

	// "tap" has 3 direct definitions with qualifiers " 1 v.", " 2 v.", " 3 n.".
	direct, fts, overflow, err := d.ResolveAllDefinitionOffsets(3, int(words3[1].LinkGroupIndex))
	if err != nil {
		t.Fatalf("ResolveAllDefinitionOffsets(tap): %v", err)
	}
	if overflow || len(fts) != 0 || len(direct) != 3 {
		t.Fatalf("tap links: direct=%v fts=%v overflow=%v", direct, fts, overflow)
	}
	wantQuals := []string{" 1 v.", " 2 v.", " 3 n."}
	for i, off := range direct {
		rec, err := d.ReadDefinition(off)
		if err != nil {
			t.Fatalf("ReadDefinition(%d): %v", off, err)
		}
		if rec.Pass != 2 || len(rec.Headwords) != 1 || rec.Headwords[0].Headword != "tap" || rec.Headwords[0].Qualifier != wantQuals[i] {
			t.Fatalf("tap def %d mismatch: %+v", i, rec)
		}
	}

	// Check L=3 Sub-stream 2 FTS-only words and verify each resolves to >= 1 occurrence.
	fts3Set := make(map[string]bool)
	for fe, err := range d.ScanFTSOnlyWords(3) {
		if err != nil {
			t.Fatalf("ScanFTSOnlyWords(3): %v", err)
		}
		fts3Set[string(fe.Canonical)] = true
		occs, err := d.ReadFTSOccurrences(fe.OccListOffset)
		if err != nil || len(occs) == 0 {
			t.Fatalf("ReadFTSOccurrences(%s @ %d): occs=%v err=%v", fe.Word, fe.OccListOffset, occs, err)
		}
	}
	for _, want := range []string{"AND", "FOR", "RTF", "WEB"} {
		if !fts3Set[want] {
			t.Fatalf("L=3 FTS-only words missing %q (got %v)", want, fts3Set)
		}
	}

	// Check Pass 1 alias-only stub ";elusive" in L=7.
	foundElusive := false
	for w, err := range d.ScanWords(7) {
		if err != nil {
			t.Fatal(err)
		}
		if w.Word == "elusive" {
			foundElusive = true
			d7, _, _, err := d.ResolveAllDefinitionOffsets(7, int(w.LinkGroupIndex))
			if err != nil {
				t.Fatal(err)
			}
			if len(d7) != 1 {
				t.Fatalf("elusive direct definitions = %v, want 1", d7)
			}
			rec, err := d.ReadDefinition(d7[0])
			if err != nil {
				t.Fatal(err)
			}
			if rec.Pass != 1 || rec.Body != "See evasive" {
				t.Fatalf("expected Pass 1 alias record for 'elusive', got %+v", rec)
			}
		}
	}
	if !foundElusive {
		t.Fatalf("did not find alias word 'elusive' in L=7")
	}

	// Check L=12 ("Crossword Man") hybrid direct + indirect FTS link group.
	var words12 []tsd.WordEntry
	for w, err := range d.ScanWords(12) {
		if err != nil {
			t.Fatal(err)
		}
		words12 = append(words12, w)
	}
	if len(words12) != 1 || words12[0].Word != "Crossword Man" {
		t.Fatalf("L=12 words = %+v", words12)
	}
	d12, f12, ov12, err := d.ResolveAllDefinitionOffsets(12, int(words12[0].LinkGroupIndex))
	if err != nil {
		t.Fatal(err)
	}
	if ov12 || len(d12) != 1 || len(f12) != 1 {
		t.Fatalf("Crossword Man links: direct=%v fts=%v overflow=%v", d12, f12, ov12)
	}
	rtfRec, err := d.ReadDefinition(f12[0])
	if err != nil {
		t.Fatal(err)
	}
	if !rtfRec.IsRTF || rtfRec.Headwords[0].Headword != "TEA Support" {
		t.Fatalf("expected FTS occurrence on TEA Support RTF definition, got %+v", rtfRec)
	}
}

func TestDecodeAllRepositoryDictionaries(t *testing.T) {
	requireDictionariesDir(t)

	allFiles, err := filepath.Glob(filepath.Join(dictionariesDir, "*.tsd"))
	if err != nil {
		t.Fatal(err)
	}
	if len(allFiles) == 0 {
		t.Fatalf("expected at least 1 .tsd dictionary in %s, found 0", dictionariesDir)
	}

	for _, path := range allFiles {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()

			d, err := tsd.Open(path)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer d.Close()

			if !utf8.ValidString(d.Description()) {
				t.Errorf("invalid UTF-8 in Description")
			}

			// Verify sequential definition scan covers the entire Definitions section cleanly.
			defCount := 0
			for rec, err := range d.ScanDefinitions(0) {
				if err != nil {
					t.Fatalf("ScanDefinitions(0) after %d defs: %v", defCount, err)
				}
				if !utf8.ValidString(rec.Body) {
					t.Fatalf("invalid UTF-8 in definition at %d", rec.Offset)
				}
				defCount++
			}
			if d.HasDefinitions() && defCount == 0 {
				t.Errorf("HasDefinitions()=true but ScanDefinitions yielded 0 records")
			}

			// Verify every word stream (both Sub-stream 1 and Sub-stream 2), LinkTable, and FTS pointer table.
			var totalDecoded uint64
			for L := 1; L <= d.MaxWordLength(); L++ {
				wantWC := d.WordCount(L)
				if wantWC == 0 {
					continue
				}
				var gotWC uint32
				var linkedCount int
				var gotFTS int

				for entry, err := range d.ScanAllRawWords(L) {
					if err != nil {
						t.Fatalf("L=%d ScanAllRawWords: %v", L, err)
					}
					if entry.IsFTSOnly {
						gotFTS++
					} else {
						gotWC++
						if entry.LinkGroupIndex >= 0 {
							linkedCount++
						}
						if gotWC <= 16 {
							s := tsd.DecodeCP1252(entry.WordBytes)
							if !utf8.ValidString(s) {
								t.Fatalf("L=%d invalid UTF-8 word", L)
							}
						}
					}
				}

				if gotWC != wantWC {
					t.Fatalf("L=%d: decoded %d words, want %d", L, gotWC, wantWC)
				}
				if gotFTS != d.FTSOnlyWordCount(L) {
					t.Fatalf("L=%d: decoded %d FTS words, want %d", L, gotFTS, d.FTSOnlyWordCount(L))
				}
				totalDecoded += uint64(gotWC)

				if d.Version() == tsd.VersionTSD1 {
					lt, err := d.ReadLinkTable(L)
					if err != nil {
						t.Fatalf("L=%d ReadLinkTable: %v", L, err)
					}
					if lt.NumGroups() != linkedCount {
						t.Fatalf("L=%d LinkTable groups=%d, linked words=%d", L, lt.NumGroups(), linkedCount)
					}
					// Resolve first and last link groups and verify their definitions/FTS lists decode.
					if lt.NumGroups() > 0 {
						for _, gIdx := range []int{0, lt.NumGroups() - 1} {
							res, err := lt.Resolve(gIdx)
							if err != nil {
								t.Fatalf("L=%d lt.Resolve(%d): %v", L, gIdx, err)
							}
							for _, doff := range res.DirectOffsets {
								if _, err := d.ReadDefinition(doff); err != nil {
									t.Fatalf("L=%d ReadDefinition(%d): %v", L, doff, err)
								}
							}
							if res.FTSListOffset != 0 && !res.FTSOverflow {
								if _, err := d.ReadFTSOccurrences(res.FTSListOffset); err != nil {
									t.Fatalf("L=%d ReadFTSOccurrences(%d): %v", L, res.FTSListOffset, err)
								}
							}
						}
					}

					ptrs, err := d.ReadFTSIndexPointers(L)
					if err != nil {
						t.Fatalf("L=%d ReadFTSIndexPointers: %v", L, err)
					}
					if len(ptrs) != gotFTS {
						t.Fatalf("L=%d FTS ptrs=%d, gotFTS=%d", L, len(ptrs), gotFTS)
					}
					if len(ptrs) > 0 {
						for _, pIdx := range []int{0, len(ptrs) - 1} {
							off := ptrs[pIdx]
							if off != tsd.FTSOverflowSentinel {
								if _, err := d.ReadFTSOccurrences(off); err != nil {
									t.Fatalf("L=%d FTS ptr[%d]=%d: %v", L, pIdx, off, err)
								}
							}
						}
					}
				}
			}

			if totalDecoded != d.TotalWords() {
				t.Fatalf("totalDecoded=%d != TotalWords()=%d", totalDecoded, d.TotalWords())
			}
		})
	}
}

func TestConcurrentDictionaryAccess(t *testing.T) {
	requireDictionariesDir(t)

	d, err := tsd.Open(filepath.Join(dictionariesDir, "english-wordnet-2025-plus.tsd"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			for L := (worker % 5) + 3; L <= d.MaxWordLength(); L += 5 {
				checked := 0
				for w, err := range d.ScanWords(L) {
					if err != nil {
						t.Errorf("worker %d L=%d ScanWords: %v", worker, L, err)
						return
					}
					if w.LinkGroupIndex >= 0 && checked < 5 {
						direct, fts, _, err := d.ResolveAllDefinitionOffsets(L, int(w.LinkGroupIndex))
						if err != nil {
							t.Errorf("worker %d ResolveAllDefinitionOffsets: %v", worker, err)
							return
						}
						for _, off := range direct {
							if _, err := d.ReadDefinition(off); err != nil {
								t.Errorf("worker %d ReadDefinition: %v", worker, err)
								return
							}
						}
						if len(fts) > 0 {
							if _, err := d.ReadDefinition(fts[0]); err != nil {
								t.Errorf("worker %d ReadDefinition(fts): %v", worker, err)
								return
							}
						}
						checked++
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestBuildAndExportFileAtomicNoTmpLeak(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "words.txt")
	if err := os.WriteFile(srcPath, []byte("alpha\nbeta|second letter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tsdPath := filepath.Join(dir, "out.tsd")
	if _, err := tsd.BuildFile(srcPath, tsdPath, tsd.DefaultBuildOptions()); err != nil {
		t.Fatalf("BuildFile: %v", err)
	}

	d, err := tsd.Open(tsdPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	exportPath := filepath.Join(dir, "exported.txt")
	victimPath := filepath.Join(dir, "victim.txt")
	if err := os.WriteFile(victimPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink(victimPath, exportPath+".tmp")

	if _, err := d.ExportFile(exportPath, tsd.DefaultExportOptions(tsd.ExportSimple)); err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	gotVictim, err := os.ReadFile(victimPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotVictim) != "keep" {
		t.Fatalf("predictable .tmp symlink target clobbered: %q", string(gotVictim))
	}

	_ = os.Remove(exportPath + ".tmp")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("unexpected leftover temp file: %s", e.Name())
		}
	}
}

type chunkedReaderAt struct {
	data    []byte
	maxRead int
}

func (c chunkedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= int64(len(c.data)) {
		return 0, io.EOF
	}
	rem := c.data[off:]
	n := len(p)
	if n > c.maxRead {
		n = c.maxRead
	}
	if n > len(rem) {
		n = len(rem)
	}
	copy(p[:n], rem[:n])
	if int(off)+n >= len(c.data) && n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestReaderHardeningAgainstCorruptHeadersAndShortReads(t *testing.T) {
	if _, err := tsd.OpenReaderAt(nil, 100, "nil"); !errors.Is(err, tsd.ErrInvalidHeader) {
		t.Fatalf("expected ErrInvalidHeader for nil ReaderAt, got %v", err)
	}

	validBytes, _, err := tsd.BuildBytes(
		[]byte("a|letter a\ncat|feline animal\ndog|canine animal\n"),
		tsd.DefaultBuildOptions(),
	)
	if err != nil {
		t.Fatalf("BuildBytes: %v", err)
	}

	// num_lengths for maxLen=3 is 4; in TSD1 header:
	// [0..6): magic + numLengths
	// [6..18): defPassOffsets[3]
	// [18..18+16): occIndexOffsets[0..3]
	// [34..50): defLinkOffsets[0..3]
	// [50..66): defLinkCounts[0..3]
	// [66..82): wordStreamOffsets[0..3]
	// [82..98): wordCounts[0..3]
	t.Run("zero_wordcount_with_nonzero_link_count_rejected", func(t *testing.T) {
		corrupt := append([]byte(nil), validBytes...)
		// Length 2 is empty (wordCounts[2] == 0); inject defLinkCounts[2] = 0xFFFFFFFF.
		binary.LittleEndian.PutUint32(corrupt[50+2*4:], 0xFFFFFFFF)
		if _, err := tsd.OpenBytes(corrupt, "corrupt"); !errors.Is(err, tsd.ErrInvalidHeader) {
			t.Fatalf("expected ErrInvalidHeader, got %v", err)
		}
	})

	t.Run("inflated_wordcount_exceeding_bitstream_rejected", func(t *testing.T) {
		corrupt := append([]byte(nil), validBytes...)
		binary.LittleEndian.PutUint32(corrupt[82+1*4:], 0x7FFFFFFF)
		if _, err := tsd.OpenBytes(corrupt, "inflated"); !errors.Is(err, tsd.ErrInvalidHeader) {
			t.Fatalf("expected ErrInvalidHeader for inflated word_counts, got %v", err)
		}
	})

	t.Run("truncated_underlying_reader_terminates_read_definition", func(t *testing.T) {
		dFull, err := tsd.OpenBytes(validBytes, "full")
		if err != nil {
			t.Fatalf("OpenBytes: %v", err)
		}
		defStart := dFull.DefPassOffsets()[0]
		// Claim full size in OpenReaderAt, then truncate backing slice inside definitions after open.
		truncBuf := append([]byte(nil), validBytes...)
		r := &chunkedReaderAt{data: truncBuf, maxRead: 4096}
		d, err := tsd.OpenReaderAt(r, int64(len(validBytes)), "trunc")
		if err != nil {
			t.Fatalf("OpenReaderAt: %v", err)
		}
		r.data = truncBuf[:int(defStart)+3]
		if _, err := d.ReadDefinition(defStart); !errors.Is(err, tsd.ErrInvalidOffset) {
			t.Fatalf("expected ErrInvalidOffset on truncated definition read, got %v", err)
		}
	})

	t.Run("chunked_short_reads_in_scan_definitions", func(t *testing.T) {
		d, err := tsd.OpenReaderAt(chunkedReaderAt{data: validBytes, maxRead: 5}, int64(len(validBytes)), "chunked")
		if err != nil {
			t.Fatalf("OpenReaderAt: %v", err)
		}
		count := 0
		for _, sErr := range d.ScanDefinitions(0) {
			if sErr != nil {
				t.Fatalf("ScanDefinitions short read error: %v", sErr)
			}
			count++
		}
		if count != 3 {
			t.Fatalf("ScanDefinitions count = %d, want 3", count)
		}
	})
}
