package tsd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ErrCorruptBitstream indicates truncated or invalid Huffman-compressed data in a .tsd word stream.
var ErrCorruptBitstream = errors.New("tsd: corrupt Huffman word bitstream")

// huffmanDecoder decodes MSB-defined / LSB-packed .tsd Huffman symbol streams
// using a 256-entry 8-bit lookahead table backed by a binary tree for codes of length 9..32.
type huffmanDecoder struct {
	fastSym  [256]uint8
	fastLen  [256]uint8
	nodes    [][2]int32 // >0: internal node index; <0: -1 - int32(symbol); 0: unassigned
	hasCodes bool
}

func reverseBits(code uint32, length uint8) uint32 {
	var r uint32
	for i := uint8(0); i < length; i++ {
		r = (r << 1) | (code & 1)
		code >>= 1
	}
	return r
}

func newHuffmanDecoder(charTable *[256]CharEntry) (*huffmanDecoder, error) {
	dec := &huffmanDecoder{
		nodes: make([][2]int32, 1, 512), // index 0 is root
	}
	for b := 0; b < 256; b++ {
		hlen := charTable[b].HuffCodeLen
		if hlen == 0 {
			continue
		}
		if hlen > 32 {
			return nil, fmt.Errorf("%w: symbol 0x%02X has Huffman code length %d > 32", ErrInvalidHeader, b, hlen)
		}
		dec.hasCodes = true
		hcode := charTable[b].HuffCode

		cur := int32(0)
		for bitIdx := int(hlen) - 1; bitIdx >= 0; bitIdx-- {
			bit := (hcode >> bitIdx) & 1
			next := dec.nodes[cur][bit]
			if bitIdx == 0 {
				if next != 0 {
					return nil, fmt.Errorf("%w: prefix collision at symbol 0x%02X (len %d)", ErrInvalidHeader, b, hlen)
				}
				dec.nodes[cur][bit] = -1 - int32(b)
			} else {
				if next < 0 {
					return nil, fmt.Errorf("%w: leaf collision along prefix of symbol 0x%02X", ErrInvalidHeader, b)
				}
				if next == 0 {
					next = int32(len(dec.nodes))
					dec.nodes = append(dec.nodes, [2]int32{})
					dec.nodes[cur][bit] = next
				}
				cur = next
			}
		}

		if hlen <= 8 {
			rev := reverseBits(hcode, hlen)
			fillCount := 1 << (8 - hlen)
			for hi := 0; hi < fillCount; hi++ {
				idx := int(rev) | (hi << hlen)
				dec.fastSym[idx] = byte(b)
				dec.fastLen[idx] = hlen
			}
		}
	}
	return dec, nil
}

const bitReaderChunkSize = 8192

// bitStreamReader reads LSB-first bits and Huffman symbols from an io.ReaderAt region.
type bitStreamReader struct {
	r       io.ReaderAt
	filePos int64
	endPos  int64

	buf       [bitReaderChunkSize]byte
	bufIdx    int
	bufLen    int
	bitBuf    uint64
	bitsAvail uint
	bitsRead  uint64
}

func (br *bitStreamReader) reset(r io.ReaderAt, startOffset, endOffset int64) {
	br.r = r
	br.filePos = startOffset
	br.endPos = endOffset
	br.bufIdx = 0
	br.bufLen = 0
	br.bitBuf = 0
	br.bitsAvail = 0
	br.bitsRead = 0
}

func (br *bitStreamReader) refill() error {
	for br.bitsAvail <= 56 {
		if br.bufIdx >= br.bufLen {
			rem := br.endPos - br.filePos
			if rem <= 0 {
				break
			}
			toRead := len(br.buf)
			if int64(toRead) > rem {
				toRead = int(rem)
			}
			n, err := br.r.ReadAt(br.buf[:toRead], br.filePos)
			if n > 0 {
				br.filePos += int64(n)
				br.bufIdx = 0
				br.bufLen = n
			}
			if n == 0 {
				if err == io.EOF {
					break
				}
				return err
			}
		}
		br.bitBuf |= uint64(br.buf[br.bufIdx]) << br.bitsAvail
		br.bufIdx++
		br.bitsAvail += 8
	}
	return nil
}

func (br *bitStreamReader) decodeSymbol(h *huffmanDecoder) (byte, error) {
	if !h.hasCodes {
		return 0, ErrCorruptBitstream
	}
	if br.bitsAvail < 32 {
		if err := br.refill(); err != nil {
			return 0, err
		}
	}
	look := uint8(br.bitBuf)
	if n := h.fastLen[look]; n != 0 && uint(n) <= br.bitsAvail {
		br.bitBuf >>= n
		br.bitsAvail -= uint(n)
		br.bitsRead += uint64(n)
		return h.fastSym[look], nil
	}

	cur := int32(0)
	for depth := 0; depth < 32; depth++ {
		if br.bitsAvail == 0 {
			if err := br.refill(); err != nil {
				return 0, err
			}
			if br.bitsAvail == 0 {
				return 0, fmt.Errorf("%w: unexpected EOF", ErrCorruptBitstream)
			}
		}
		bit := br.bitBuf & 1
		br.bitBuf >>= 1
		br.bitsAvail--
		br.bitsRead++

		next := h.nodes[cur][bit]
		if next == 0 {
			return 0, fmt.Errorf("%w: unassigned Huffman code bit", ErrCorruptBitstream)
		}
		if next < 0 {
			return byte(-1 - next), nil
		}
		cur = next
	}
	return 0, fmt.Errorf("%w: code exceeded 32 bits", ErrCorruptBitstream)
}

func (br *bitStreamReader) readBit() (uint8, error) {
	if br.bitsAvail == 0 {
		if err := br.refill(); err != nil {
			return 0, err
		}
		if br.bitsAvail == 0 {
			return 0, fmt.Errorf("%w: unexpected EOF reading link flag bit", ErrCorruptBitstream)
		}
	}
	b := uint8(br.bitBuf & 1)
	br.bitBuf >>= 1
	br.bitsAvail--
	br.bitsRead++
	return b, nil
}

// bitStreamWriter packs bits LSB-first into 16-bit little-endian words.
type bitStreamWriter struct {
	dst    []byte
	acc    uint16
	bitCnt uint
}

func (bw *bitStreamWriter) putBits(nbits uint8, val uint32) {
	for i := uint8(0); i < nbits; i++ {
		if (val>>i)&1 != 0 {
			bw.acc |= 1 << bw.bitCnt
		}
		bw.bitCnt++
		if bw.bitCnt == 16 {
			bw.dst = binary.LittleEndian.AppendUint16(bw.dst, bw.acc)
			bw.acc = 0
			bw.bitCnt = 0
		}
	}
}

func (bw *bitStreamWriter) flush() {
	if bw.bitCnt > 0 {
		bw.dst = binary.LittleEndian.AppendUint16(bw.dst, bw.acc)
		bw.acc = 0
		bw.bitCnt = 0
	}
}

// huffBuildNode is used during Huffman tree construction.
type huffBuildNode struct {
	left, right *huffBuildNode
	sym         byte
	freq        uint64
	codeLen     uint8
	code        uint32
}

func prependHuffBit(n *huffBuildNode, bit uint32) {
	if n.left == nil {
		n.code |= bit << n.codeLen
		n.codeLen++
		return
	}
	prependHuffBit(n.left, bit)
	prependHuffBit(n.right, bit)
}

func collectHuffLeaves(n *huffBuildNode, lengths *[256]uint8, codes *[256]uint32) {
	if n == nil {
		return
	}
	if n.left == nil {
		lengths[n.sym] = n.codeLen
		codes[n.sym] = n.code
		return
	}
	collectHuffLeaves(n.left, lengths, codes)
	collectHuffLeaves(n.right, lengths, codes)
}

func cmpHuffNodesDesc(a, b *huffBuildNode) int {
	if b.freq > a.freq {
		return 1
	}
	if b.freq < a.freq {
		return -1
	}
	return 0
}

// sortHuffNodesDesc sorts the first n Huffman nodes by descending frequency using
// an in-place median-of-three 3-way quicksort for deterministic tie-breaking.
func sortHuffNodesDesc(arr []*huffBuildNode, n int) {
	var sortRange func(base, count int)
	sortRange = func(base, count int) {
		for count >= 2 {
			if count == 2 {
				if cmpHuffNodesDesc(arr[base], arr[base+1]) > 0 {
					arr[base], arr[base+1] = arr[base+1], arr[base]
				}
				return
			}
			hi := base + count - 1
			mid := base + (count >> 1)
			lo := base
			if cmpHuffNodesDesc(arr[mid], arr[hi]) > 0 {
				arr[mid], arr[hi] = arr[hi], arr[mid]
			}
			if cmpHuffNodesDesc(arr[mid], arr[lo]) > 0 {
				arr[mid], arr[lo] = arr[lo], arr[mid]
			} else if cmpHuffNodesDesc(arr[lo], arr[hi]) > 0 {
				arr[lo], arr[hi] = arr[hi], arr[lo]
			}
			if count == 3 {
				arr[lo], arr[mid] = arr[mid], arr[lo]
				return
			}
			eqEnd := lo + 1
			mid = eqEnd
			for {
				for {
					c := cmpHuffNodesDesc(arr[mid], arr[lo])
					if c > 0 {
						break
					}
					if c == 0 {
						arr[mid], arr[eqEnd] = arr[eqEnd], arr[mid]
						eqEnd++
					}
					if hi <= mid {
						break
					}
					mid++
				}
				if cmpHuffNodesDesc(arr[mid], arr[lo]) <= 0 && hi <= mid {
					break
				}
				for hi > mid {
					c := cmpHuffNodesDesc(arr[lo], arr[hi])
					if c < 0 {
						hi--
					} else {
						arr[mid], arr[hi] = arr[hi], arr[mid]
						if c != 0 {
							mid++
							hi--
						}
						break
					}
				}
				if hi <= mid {
					break
				}
			}
			if cmpHuffNodesDesc(arr[mid], arr[lo]) <= 0 {
				mid++
			}
			swapHi := mid - 1
			s := lo
			for s < eqEnd && swapHi >= eqEnd {
				arr[s], arr[swapHi] = arr[swapHi], arr[s]
				s++
				swapHi--
			}
			leftN := mid - eqEnd
			rightN := (lo + count) - mid
			if leftN <= rightN {
				sortRange(lo, leftN)
				base = mid
				count = rightN
			} else {
				sortRange(mid, rightN)
				count = leftN
			}
		}
	}
	sortRange(0, n)
}

// buildHuffmanCodes computes MSB-first Huffman code lengths and codewords from byte frequencies,
// falling back to fixed 8-bit identity codes (code = byte value) if any codeword exceeds 32 bits.
func buildHuffmanCodes(freq *[256]uint64) (lengths [256]uint8, codes [256]uint32) {
	nodes := make([]*huffBuildNode, 0, 256)
	for b := 0; b < 256; b++ {
		if freq[b] > 0 {
			nodes = append(nodes, &huffBuildNode{
				sym:  byte(b),
				freq: freq[b],
			})
		}
	}
	numLeaves := len(nodes)
	if numLeaves == 0 {
		return lengths, codes
	}
	if numLeaves == 1 {
		lengths[nodes[0].sym] = 1
		codes[nodes[0].sym] = 0
		return lengths, codes
	}

	for step := 0; step < numLeaves-1; step++ {
		rem := numLeaves - step
		sortHuffNodesDesc(nodes, rem)
		prependHuffBit(nodes[rem-2], 0)
		prependHuffBit(nodes[rem-1], 1)
		nodes[rem-2] = &huffBuildNode{
			left:  nodes[rem-2],
			right: nodes[rem-1],
			freq:  nodes[rem-2].freq + nodes[rem-1].freq,
		}
		nodes[rem-1] = nil
	}

	collectHuffLeaves(nodes[0], &lengths, &codes)
	for b := 0; b < 256; b++ {
		if lengths[b] > 32 {
			for k := 0; k < 256; k++ {
				lengths[k] = 8
				codes[k] = uint32(k)
			}
			break
		}
	}
	return lengths, codes
}
