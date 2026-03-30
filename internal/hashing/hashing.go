package hashing

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"sync"

	mmap "github.com/edsrzf/mmap-go"
	"github.com/zeebo/blake3"
	"github.com/zeebo/xxh3"
)

var partialBufPool = sync.Pool{New: func() any { b := make([]byte, 4<<10); return &b }}
var fullBufPool = sync.Pool{New: func() any { b := make([]byte, 256<<10); return &b }}

func Partial(path string) (string, error) {
	const chunk = 4 << 10

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()

	h := sha256.New()
	bufp := partialBufPool.Get().(*[]byte)
	buf := *bufp
	defer partialBufPool.Put(bufp)

	n, _ := f.Read(buf)
	_, _ = h.Write(buf[:n])

	if size > 2*chunk {
		_, _ = f.Seek(size/2-chunk/2, io.SeekStart)
		n2, _ := f.Read(buf)
		_, _ = h.Write(buf[:n2])
	}

	if size > chunk {
		_, _ = f.Seek(size-chunk, io.SeekStart)
		n3, _ := f.Read(buf)
		_, _ = h.Write(buf[:n3])
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func fullRead(path, algo string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	bufp := fullBufPool.Get().(*[]byte)
	buf := *bufp
	defer fullBufPool.Put(bufp)

	switch algo {
	case "sha256":
		h := sha256.New()
		if _, err := io.CopyBuffer(h, f, buf); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	case "xxh3":
		h := xxh3.New()
		if _, err := io.CopyBuffer(h, f, buf); err != nil {
			return "", err
		}
		return fmt.Sprintf("%x", h.Sum64()), nil
	case "blake3":
		h := blake3.New()
		if _, err := io.CopyBuffer(h, f, buf); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	default:
		return "", errors.New("unknown hash algorithm: " + algo)
	}
}

func fullMmap(path, algo string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	mm, err := mmap.Map(f, mmap.RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer mm.Unmap()

	switch algo {
	case "sha256":
		var h hash.Hash = sha256.New()
		_, _ = h.Write(mm)
		return hex.EncodeToString(h.Sum(nil)), nil
	case "xxh3":
		return fmt.Sprintf("%x", xxh3.Hash(mm)), nil
	case "blake3":
		h := blake3.New()
		_, _ = h.Write(mm)
		return hex.EncodeToString(h.Sum(nil)), nil
	default:
		return "", errors.New("unknown hash algorithm: " + algo)
	}
}

func Full(path string, useMmap bool, algo string) (string, error) {
	if useMmap {
		if h, err := fullMmap(path, algo); err == nil {
			return h, nil
		}
	}
	return fullRead(path, algo)
}
