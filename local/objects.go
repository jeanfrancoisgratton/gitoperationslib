package local

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	gol "github.com/jeanfrancoisgratton/gitoperationslib"
)

// ErrObjectNotFound is returned for an object the repository does not have.
var ErrObjectNotFound = errors.New("object not found")

// Object types, as numbered in pack files.
const (
	objCommit   = 1
	objTree     = 2
	objBlob     = 3
	objTag      = 4
	objOfsDelta = 6
	objRefDelta = 7
)

var typeNames = map[string]int{"commit": objCommit, "tree": objTree, "blob": objBlob, "tag": objTag}

// objectStore reads objects: loose ones, then pack files, in the repository's
// objects directory and its alternates.
type objectStore struct {
	mu    sync.Mutex
	dirs  []string // objects directories
	packs []*pack
	cache map[packPos]object // delta bases
}

type object struct {
	typ  int
	data []byte
}

type packPos struct {
	p   *pack
	off int64
}

type pack struct {
	f       *os.File
	fanout  [256]uint32
	shas    []byte // count × 20, sorted
	offsets []byte // count × 4
	large   []byte // 8-byte offsets, for packs over 2 GiB
}

func (r *Repo) objects() (*objectStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store != nil {
		return r.store, nil
	}
	cfg, err := r.config()
	if err != nil {
		return nil, err
	}
	if f := strings.ToLower(cfg.get("extensions.objectformat")); f != "" && f != "sha1" {
		return nil, fmt.Errorf("%w: %s object format", gol.ErrNotSupported, f)
	}
	s := &objectStore{cache: map[packPos]object{}}
	s.addDir(filepath.Join(r.commonDir, "objects"), 0)
	for _, d := range s.dirs {
		names, _ := filepath.Glob(filepath.Join(d, "pack", "*.idx"))
		sort.Strings(names)
		for _, n := range names {
			p, err := openPack(n)
			if err != nil {
				s.close()
				return nil, err
			}
			s.packs = append(s.packs, p)
		}
	}
	r.store = s
	return s, nil
}

// addDir adds an objects directory and, recursively, its alternates.
func (s *objectStore) addDir(dir string, depth int) {
	s.dirs = append(s.dirs, dir)
	b, err := os.ReadFile(filepath.Join(dir, "info", "alternates"))
	if err != nil || depth > 5 {
		return
	}
	for line := range strings.Lines(string(b)) {
		if line = strings.TrimSpace(line); line != "" && line[0] != '#' {
			s.addDir(resolvePath(dir, line), depth+1)
		}
	}
}

func (s *objectStore) close() {
	for _, p := range s.packs {
		p.f.Close()
	}
}

// Close releases the pack files opened to read commits. A Repo can still be
// used after it: they are opened again when needed.
func (r *Repo) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.store != nil {
		r.store.close()
		r.store = nil
	}
	return nil
}

func openPack(idxPath string) (*pack, error) {
	idx, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, err
	}
	if len(idx) < 8+256*4 || !bytes.Equal(idx[:4], []byte{0xff, 't', 'O', 'c'}) || binary.BigEndian.Uint32(idx[4:]) != 2 {
		return nil, fmt.Errorf("%s: not a version 2 pack index", idxPath)
	}
	p := &pack{}
	for i := range p.fanout {
		p.fanout[i] = binary.BigEndian.Uint32(idx[8+4*i:])
	}
	n := int(p.fanout[255])
	at := 8 + 256*4
	if len(idx) < at+n*(20+4+4) {
		return nil, fmt.Errorf("%s: truncated", idxPath)
	}
	p.shas = idx[at : at+n*20]
	at += n * 20
	at += n * 4 // CRCs
	p.offsets = idx[at : at+n*4]
	p.large = idx[at+n*4:]
	if p.f, err = os.Open(strings.TrimSuffix(idxPath, ".idx") + ".pack"); err != nil {
		return nil, err
	}
	return p, nil
}

// find returns the offset of an object in the pack, or -1.
func (p *pack) find(sha []byte) int64 {
	lo := 0
	if sha[0] > 0 {
		lo = int(p.fanout[sha[0]-1])
	}
	hi := int(p.fanout[sha[0]])
	i := lo + sort.Search(hi-lo, func(i int) bool { return bytes.Compare(p.shas[(lo+i)*20:(lo+i)*20+20], sha) >= 0 })
	if i >= hi || !bytes.Equal(p.shas[i*20:i*20+20], sha) {
		return -1
	}
	off := binary.BigEndian.Uint32(p.offsets[i*4:])
	if off&0x80000000 != 0 {
		j := int(off &^ 0x80000000)
		if len(p.large) < j*8+8 {
			return -1
		}
		return int64(binary.BigEndian.Uint64(p.large[j*8:]))
	}
	return int64(off)
}

// read returns the object with the given hex SHA.
func (s *objectStore) read(sha string) (object, error) {
	raw, err := hex.DecodeString(sha)
	if err != nil || len(raw) != 20 {
		return object{}, fmt.Errorf("%q is not a SHA-1", sha)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readRaw(raw, sha)
}

func (s *objectStore) readRaw(raw []byte, sha string) (object, error) {
	for _, p := range s.packs {
		if off := p.find(raw); off >= 0 {
			return s.packed(p, off, 0)
		}
	}
	for _, d := range s.dirs {
		o, err := readLoose(filepath.Join(d, sha[:2], sha[2:]))
		if !errors.Is(err, fs.ErrNotExist) {
			return o, err
		}
	}
	return object{}, fmt.Errorf("%w: %s", ErrObjectNotFound, sha)
}

func readLoose(path string) (object, error) {
	f, err := os.Open(path)
	if err != nil {
		return object{}, err
	}
	defer f.Close()
	z, err := zlib.NewReader(bufio.NewReader(f))
	if err != nil {
		return object{}, fmt.Errorf("%s: %w", path, err)
	}
	b, err := io.ReadAll(z)
	if err != nil {
		return object{}, fmt.Errorf("%s: %w", path, err)
	}
	header, data, ok := bytes.Cut(b, []byte{0})
	name, _, _ := strings.Cut(string(header), " ")
	if !ok || typeNames[name] == 0 {
		return object{}, fmt.Errorf("%s: not a git object", path)
	}
	return object{typ: typeNames[name], data: data}, nil
}

const maxDeltaDepth = 1000

// packed reads the object at off, resolving deltas.
func (s *objectStore) packed(p *pack, off int64, depth int) (object, error) {
	if o, ok := s.cache[packPos{p, off}]; ok {
		return o, nil
	}
	if depth > maxDeltaDepth {
		return object{}, errors.New("pack: delta chain too long")
	}
	var hdr [32]byte
	n, err := p.f.ReadAt(hdr[:], off)
	if n == 0 {
		return object{}, fmt.Errorf("pack: %w", err)
	}
	c := hdr[0]
	typ := int(c>>4) & 7
	size := int64(c & 0x0f)
	i, shift := 1, 4
	for c&0x80 != 0 {
		if i >= n {
			return object{}, errors.New("pack: bad object header")
		}
		c = hdr[i]
		size |= int64(c&0x7f) << shift
		i++
		shift += 7
	}
	var base object
	switch typ {
	case objOfsDelta:
		if i >= n {
			return object{}, errors.New("pack: bad delta header")
		}
		c = hdr[i]
		i++
		back := int64(c & 0x7f)
		for c&0x80 != 0 {
			if i >= n {
				return object{}, errors.New("pack: bad delta offset")
			}
			c = hdr[i]
			i++
			back = (back+1)<<7 | int64(c&0x7f)
		}
		if base, err = s.packed(p, off-back, depth+1); err != nil {
			return object{}, err
		}
	case objRefDelta:
		if i+20 > n {
			return object{}, errors.New("pack: bad delta base")
		}
		raw := append([]byte(nil), hdr[i:i+20]...)
		i += 20
		if base, err = s.readRaw(raw, hex.EncodeToString(raw)); err != nil {
			return object{}, err
		}
	case objCommit, objTree, objBlob, objTag:
	default:
		return object{}, fmt.Errorf("pack: unknown object type %d", typ)
	}
	z, err := zlib.NewReader(bufio.NewReader(io.NewSectionReader(p.f, off+int64(i), 1<<62)))
	if err != nil {
		return object{}, fmt.Errorf("pack: %w", err)
	}
	data := make([]byte, size)
	if _, err = io.ReadFull(z, data); err != nil {
		return object{}, fmt.Errorf("pack: %w", err)
	}
	o := object{typ: typ, data: data}
	if typ == objOfsDelta || typ == objRefDelta {
		if o.data, err = applyDelta(base.data, data); err != nil {
			return object{}, err
		}
		o.typ = base.typ
	}
	if len(s.cache) > 1024 {
		clear(s.cache)
	}
	s.cache[packPos{p, off}] = o
	return o, nil
}

func applyDelta(base, delta []byte) ([]byte, error) {
	bad := errors.New("pack: bad delta")
	varint := func() (int, bool) {
		v, shift := 0, 0
		for {
			if len(delta) == 0 {
				return 0, false
			}
			c := delta[0]
			delta = delta[1:]
			v |= int(c&0x7f) << shift
			shift += 7
			if c&0x80 == 0 {
				return v, true
			}
		}
	}
	srcSize, ok1 := varint()
	dstSize, ok2 := varint()
	if !ok1 || !ok2 || srcSize != len(base) {
		return nil, bad
	}
	out := make([]byte, 0, dstSize)
	for len(delta) > 0 {
		c := delta[0]
		delta = delta[1:]
		if c&0x80 != 0 {
			var off, size int
			for b := range 7 {
				if c&(1<<b) == 0 {
					continue
				}
				if len(delta) == 0 {
					return nil, bad
				}
				if b < 4 {
					off |= int(delta[0]) << (8 * b)
				} else {
					size |= int(delta[0]) << (8 * (b - 4))
				}
				delta = delta[1:]
			}
			if size == 0 {
				size = 0x10000
			}
			if off+size > len(base) {
				return nil, bad
			}
			out = append(out, base[off:off+size]...)
		} else if c != 0 {
			if int(c) > len(delta) {
				return nil, bad
			}
			out = append(out, delta[:c]...)
			delta = delta[c:]
		} else {
			return nil, bad
		}
	}
	if len(out) != dstSize {
		return nil, bad
	}
	return out, nil
}
