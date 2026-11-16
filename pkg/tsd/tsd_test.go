package tsd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"unicode/utf8"

	"tisane/pkg/tsd"
)

var binaryDir = filepath.Join("..", "..", "binary")

func requireBinaryDir(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(binaryDir); os.IsNotExist(err) {
		t.Skipf("binary test corpus not present at %s", binaryDir)
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

func TestLoadAllTSMFiles(t *testing.T) {
	requireBinaryDir(t)

	defaultLoaded, err := tsd.LoadTSM(filepath.Join(binaryDir, "Default.TSM"))
	if err != nil {
		t.Fatalf("LoadTSM(Default.TSM): %v", err)
	}
	if defaultLoaded != tsd.DefaultTSM() {
		t.Fatalf("Default.TSM does not match built-in DefaultTSM()")
	}

	expectedClasses := map[string]int{
		"ANSI.TSM":     63,
		"ANSIACa.TSM":  252,
		"ANSIAcce.TSM": 95,
		"ANSIAll.TSM":  166,
		"ANSICase.TSM": 152,
		"Baltic.TSM":   61,
		"CentEuro.TSM": 60,
		"Default.TSM":  63,
		"Slovene.TSM":  65,
		"Turkish.TSM":  63,
	}

	matches, err := filepath.Glob(filepath.Join(binaryDir, "*.TSM"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != len(expectedClasses) {
		t.Fatalf("found %d .TSM files, expected %d", len(matches), len(expectedClasses))
	}

	for _, p := range matches {
		base := filepath.Base(p)
		m, err := tsd.LoadTSM(p)
		if err != nil {
			t.Fatalf("LoadTSM(%s): %v", base, err)
		}
		if got, want := m.NumClasses(), expectedClasses[base]; got != want {
			t.Errorf("%s: NumClasses()=%d, want %d", base, got, want)
		}
		if m.IsAlpha(0x00) || m.IsAlpha(0x0A) || m.IsAlpha(0x0D) {
			t.Errorf("%s: mandatory non-alphabetic control byte marked alphabetic", base)
		}
	}
}

func TestTSBuild2DetailedWalkthrough(t *testing.T) {
	requireBinaryDir(t)

	d, err := tsd.Open(filepath.Join(binaryDir, "tsbuild2.tsd"))
	if err != nil {
		t.Fatalf("Open(tsbuild2.tsd): %v", err)
	}
	defer d.Close()

	if d.Version() != tsd.VersionTSD1 {
		t.Fatalf("Version() = %s, want TSD1", d.Version())
	}
	if d.Description() != "TEA&Sympathy Dictionary Builder Example 2" {
		t.Fatalf("Description() = %q", d.Description())
	}
	if d.TotalWords() != 21 || d.MaxWordLength() != 12 {
		t.Fatalf("TotalWords=%d MaxWordLength=%d, want 21 and 12", d.TotalWords(), d.MaxWordLength())
	}
	if !d.HasDefinitions() || !d.HasPass2Definitions() || !d.HasFTSIndex() {
		t.Fatalf("expected definitions and FTS index to be reported present")
	}

	// Check L=3 dictionary words ("aid", "tap") and FTS-only words.
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

	// "tap" (LinkGroupIndex 1) has 3 direct definitions with qualifiers " 1 v.", " 2 v.", " 3 n.".
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

	// Check L=3 Sub-stream 2 FTS-only words.
	var fts3 []string
	for fe, err := range d.ScanFTSOnlyWords(3) {
		if err != nil {
			t.Fatalf("ScanFTSOnlyWords(3): %v", err)
		}
		fts3 = append(fts3, fe.Word)
		occs, err := d.ReadFTSOccurrences(fe.OccListOffset)
		if err != nil || len(occs) == 0 {
			t.Fatalf("ReadFTSOccurrences(%s @ %d): occs=%v err=%v", fe.Word, fe.OccListOffset, occs, err)
		}
	}
	wantFTS3 := []string{"adJ", "and", "CoM", "for", "has", "rtf", "Web", "WWW"}
	if len(fts3) != len(wantFTS3) {
		t.Fatalf("L=3 FTS words = %v, want %v", fts3, wantFTS3)
	}
	for i := range wantFTS3 {
		if fts3[i] != wantFTS3[i] {
			t.Fatalf("L=3 FTS[%d] = %q, want %q (all=%v)", i, fts3[i], wantFTS3[i], fts3)
		}
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
	requireBinaryDir(t)

	topFiles, err := filepath.Glob(filepath.Join(binaryDir, "*.tsd"))
	if err != nil {
		t.Fatal(err)
	}
	subFiles, err := filepath.Glob(filepath.Join(binaryDir, "Dictionaries", "*.tsd"))
	if err != nil {
		t.Fatal(err)
	}
	allFiles := append(topFiles, subFiles...)
	if len(allFiles) < 30 {
		t.Fatalf("expected at least 30 .tsd dictionaries, found %d", len(allFiles))
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
	requireBinaryDir(t)

	d, err := tsd.Open(filepath.Join(binaryDir, "Dictionaries", "The Devil's Dictionary.tsd"))
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
