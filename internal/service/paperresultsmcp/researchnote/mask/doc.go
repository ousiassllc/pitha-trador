package mask

import (
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// span is a byte range [start, end) of the document's current text.
type span [2]int

// doc is the text the rules run on, with enough bookkeeping to put every
// masked span back onto the text the caller wrote.
//
// The rules (ASCII regular expressions and key words) run on a folded copy:
// every rune whose NFKC form is plain ASCII (fullwidth letters, digits and
// punctuation such as ＡＰＩ_ＫＥＹ＝ and ｓｋ－, compatibility forms such as ①
// or ㎏) is replaced by that ASCII text, so ＡＰＩ_ＫＥＹ＝… is read as
// API_KEY=…. Folding can change lengths, so a masked span is never cut out of
// the folded text by offset: lo and hi map each boundary of the current text
// back to the unfolded input (lo when the boundary starts a span, hi when it
// ends one; they differ inside a rune that folded into several characters or
// inside "[masked]"), cuts collects the input ranges the rules masked, and
// original puts Replacement over exactly those ranges. Text the rules did not
// mask is returned as the caller wrote it (fullwidth and all), never folded.
type doc struct {
	text   string
	lo, hi []int
	cuts   []span
}

// newDoc folds src. src must not be empty.
func newDoc(src string) *doc {
	var b strings.Builder
	b.Grow(len(src))
	d := &doc{lo: make([]int, 0, len(src)+1), hi: make([]int, 0, len(src)+1)}
	for i := 0; i < len(src); {
		r, size := utf8.DecodeRuneInString(src[i:])
		text := src[i : i+size]
		if folded, ok := foldRune(r, size); ok {
			text = folded
		}
		for k := range len(text) {
			end := i
			if k > 0 { // inside the rune: a span ending here ends the whole rune
				end = i + size
			}
			d.lo, d.hi = append(d.lo, i), append(d.hi, end)
		}
		b.WriteString(text)
		i += size
	}
	d.lo, d.hi = append(d.lo, len(src)), append(d.hi, len(src))
	d.text = b.String()
	return d
}

// foldRune returns the ASCII text r (size bytes long in the input) is
// NFKC-equivalent to, if it is.
func foldRune(r rune, size int) (string, bool) {
	if r < utf8.RuneSelf || (r == utf8.RuneError && size == 1) {
		return "", false
	}
	folded := norm.NFKC.String(string(r))
	if folded == "" || folded == string(r) {
		return "", false
	}
	for i := range len(folded) {
		if folded[i] >= utf8.RuneSelf {
			return "", false
		}
	}
	return folded, true
}

// mask replaces each of spans (sorted, not overlapping, in the current text)
// with Replacement and records the input ranges they cover.
func (d *doc) mask(spans []span) {
	if len(spans) == 0 {
		return
	}
	var b strings.Builder
	lo, hi := make([]int, 0, len(d.lo)), make([]int, 0, len(d.hi))
	pos := 0
	for _, sp := range spans {
		from, to := sp[0], sp[1]
		b.WriteString(d.text[pos:from])
		lo, hi = append(lo, d.lo[pos:from]...), append(hi, d.hi[pos:from]...)
		d.cuts = append(d.cuts, span{d.lo[from], d.hi[to]})
		b.WriteString(Replacement)
		lo, hi = append(lo, d.lo[from]), append(hi, d.hi[from])
		for range len(Replacement) - 1 {
			lo, hi = append(lo, d.lo[from]), append(hi, d.hi[to])
		}
		pos = to
	}
	b.WriteString(d.text[pos:])
	d.text = b.String()
	d.lo, d.hi = append(lo, d.lo[pos:]...), append(hi, d.hi[pos:]...)
}

// original returns src with Replacement over every masked range; src is the
// text the doc was built from. Ranges that overlap (a later rule masking more
// of an already masked value) become one.
func (d *doc) original(src string) string {
	if len(d.cuts) == 0 {
		return src
	}
	sort.Slice(d.cuts, func(i, j int) bool { return d.cuts[i][0] < d.cuts[j][0] })
	var b strings.Builder
	pos := 0
	for _, c := range d.cuts {
		if c[0] < pos {
			pos = max(pos, c[1])
			continue
		}
		b.WriteString(src[pos:c[0]])
		b.WriteString(Replacement)
		pos = c[1]
	}
	b.WriteString(src[pos:])
	return b.String()
}
