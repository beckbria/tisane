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
	"fmt"
	"io"
	"os"
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
