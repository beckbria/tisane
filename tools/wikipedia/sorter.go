package main

// sorter logically orders the words from a Wikipedia dump in a priority order.
// This allows entries spelled identically (`West Hall` vs `Westhall`) to be
// deduplicated, reducing the size of the resulting dictionary without losing
// content.
//
// The sorter algorithm is based on work by Ross Beresford (crosswordman.com,
// author of TEA) used with permission.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"tisane/pkg/wordlist"
)

const (
	tierBasic    = 1
	tierNonDigit = 2
	tierDigit    = 3
)

// TitleTier categorizes a title into one of three priority tiers so simpler,
// cleaner representations appear earlier in the output:
//   - Tier 1: basic entries containing only [a-zA-Z '!?,/.-]
//   - Tier 2: other entries that do not contain ASCII digits [0-9]
//   - Tier 3: entries containing one or more ASCII digits [0-9]
func TitleTier(title string) int {
	basic := true
	for i := 0; i < len(title); i++ {
		b := title[i]
		if b >= '0' && b <= '9' {
			return tierDigit
		}
		if basic && !isBasicTitleChar(b) {
			basic = false
		}
	}
	if basic {
		return tierBasic
	}
	return tierNonDigit
}

func isBasicTitleChar(b byte) bool {
	if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') {
		return true
	}
	switch b {
	case ' ', '\'', '!', '?', ',', '/', '.', '-':
		return true
	default:
		return false
	}
}

// TieredTitleWriter partitions titles into three priority tiers while
// preserving input order within each tier.
//
// Tier 1 titles are streamed directly to the destination writer. Tier 2 and
// tier 3 titles are spooled to temporary files and appended in tier order when
// Finalize is called.
type TieredTitleWriter struct {
	dst *bufio.Writer

	tier2File *os.File
	tier2Buf  *bufio.Writer

	tier3File *os.File
	tier3Buf  *bufio.Writer
}

func NewTieredTitleWriter(dst *bufio.Writer) *TieredTitleWriter {
	return &TieredTitleWriter{dst: dst}
}

func (tw *TieredTitleWriter) WriteTitle(title string) error {
	switch TitleTier(title) {
	case tierBasic:
		if _, err := tw.dst.WriteString(title); err != nil {
			return err
		}
		return tw.dst.WriteByte('\n')
	case tierNonDigit:
		if tw.tier2Buf == nil {
			f, err := os.CreateTemp("", "tisane-wiki-tier2-*")
			if err != nil {
				return fmt.Errorf("failed creating tier 2 temp file: %w", err)
			}
			tw.tier2File = f
			tw.tier2Buf = bufio.NewWriterSize(f, 256*1024)
		}
		if _, err := tw.tier2Buf.WriteString(title); err != nil {
			return err
		}
		return tw.tier2Buf.WriteByte('\n')
	default:
		if tw.tier3Buf == nil {
			f, err := os.CreateTemp("", "tisane-wiki-tier3-*")
			if err != nil {
				return fmt.Errorf("failed creating tier 3 temp file: %w", err)
			}
			tw.tier3File = f
			tw.tier3Buf = bufio.NewWriterSize(f, 256*1024)
		}
		if _, err := tw.tier3Buf.WriteString(title); err != nil {
			return err
		}
		return tw.tier3Buf.WriteByte('\n')
	}
}

// Finalize appends buffered tier 2 titles followed by tier 3 titles to the
// destination writer and cleans up temporary files.
func (tw *TieredTitleWriter) Finalize() error {
	defer tw.Close()

	if err := tw.appendTemp(&tw.tier2File, &tw.tier2Buf); err != nil {
		return fmt.Errorf("failed writing tier 2 titles: %w", err)
	}
	if err := tw.appendTemp(&tw.tier3File, &tw.tier3Buf); err != nil {
		return fmt.Errorf("failed writing tier 3 titles: %w", err)
	}
	return nil
}

func (tw *TieredTitleWriter) appendTemp(fp **os.File, bp **bufio.Writer) error {
	f := *fp
	bw := *bp
	if f == nil {
		return nil
	}
	defer func() {
		name := f.Name()
		_ = f.Close()
		_ = os.Remove(name)
		*fp = nil
		*bp = nil
	}()

	if err := bw.Flush(); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(tw.dst, f); err != nil {
		return err
	}
	return nil
}

// Close closes and removes any open temporary tier files. Safe to call multiple times.
func (tw *TieredTitleWriter) Close() {
	for _, pair := range []struct {
		fp **os.File
		bp **bufio.Writer
	}{
		{&tw.tier2File, &tw.tier2Buf},
		{&tw.tier3File, &tw.tier3Buf},
	} {
		if *pair.fp != nil {
			name := (*pair.fp).Name()
			_ = (*pair.fp).Close()
			_ = os.Remove(name)
			*pair.fp = nil
			*pair.bp = nil
		}
	}
}

// deduplicateTitlesFile streams tier-sorted titles from srcPath and writes
// unique titles to dstPath. Deduplication is keyed on the normalized form
// produced by wordlist.ConvertEntry(title, conv) while preserving the
// first-seen original title (e.g., keeping "Galaxy Quest" rather than
// "galaxyquest"). Titles whose normalized key is empty are omitted.
func deduplicateTitlesFile(ctx context.Context, srcPath, dstPath string, conv wordlist.Conversion) (inLines, outEntries int64, err error) {
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}

	inFile, err := os.Open(srcPath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open %q: %w", srcPath, err)
	}
	defer inFile.Close()

	outFile, bw, err := createOutputWriter(dstPath)
	if err != nil {
		return 0, 0, err
	}
	cleanup := func() {
		_ = outFile.Close()
		_ = os.Remove(dstPath)
	}

	seen := make(map[string]struct{})
	br := bufio.NewReaderSize(inFile, 256*1024)

	for {
		if inLines&0xFFF == 0 {
			if ctxErr := ctx.Err(); ctxErr != nil {
				cleanup()
				return inLines, outEntries, ctxErr
			}
		}

		line, readErr := br.ReadString('\n')
		if len(line) > 0 {
			title := strings.TrimRight(line, "\r\n")
			if title != "" {
				inLines++
				key := wordlist.ConvertEntry(title, conv)
				if key != "" {
					if _, exists := seen[key]; !exists {
						seen[key] = struct{}{}
						if _, wErr := bw.WriteString(title); wErr != nil {
							cleanup()
							return inLines, outEntries, wErr
						}
						if wErr := bw.WriteByte('\n'); wErr != nil {
							cleanup()
							return inLines, outEntries, wErr
						}
						outEntries++
					}
				}
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			cleanup()
			return inLines, outEntries, readErr
		}
	}

	if err := bw.Flush(); err != nil {
		cleanup()
		return inLines, outEntries, fmt.Errorf("failed flushing deduplicated output: %w", err)
	}
	if err := outFile.Close(); err != nil {
		_ = os.Remove(dstPath)
		return inLines, outEntries, fmt.Errorf("failed closing deduplicated output: %w", err)
	}
	return inLines, outEntries, nil
}
