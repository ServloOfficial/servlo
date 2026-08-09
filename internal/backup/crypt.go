// Package backup takes a site away from this server: its files, its database,
// and enough of servlo's own state to put it back somewhere else.
//
// What is written is one encrypted stream. Backups end up on object storage or
// another server, which is to say on a disk nobody here controls, so the
// archive is ciphertext before it leaves this process rather than after it
// arrives. The key stays on the server and is never sent with the backup.
package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// KeySize is the length of a backup key, which is an AES-256 key.
const KeySize = 32

// chunkSize is how much plaintext one sealed chunk carries.
//
// A backup is far too large to seal in one piece: AEAD wants the whole message
// in memory, and a multi-gigabyte site would not fit. Sealing in chunks is the
// standard answer, and 1 MiB is large enough that the per-chunk overhead is
// noise and small enough that a restore streams rather than buffers.
const chunkSize = 1 << 20

// magic marks the format and its version, so a future change can be recognised
// rather than guessed at from a decryption failure.
var magic = [8]byte{'S', 'E', 'R', 'V', 'B', 'A', 'K', 1}

// The two chunk kinds. Every chunk says which it is, and that byte is
// authenticated, which is what stops a truncated archive from reading as a
// complete one: cutting the stream short removes the final chunk, and without
// it the reader reaches EOF still waiting for an end it was promised.
const (
	chunkMore  byte = 0
	chunkFinal byte = 1
)

// Encryptor seals a stream in chunks. Close writes the final chunk and must be
// called, or what lands on disk is an archive no decryptor will accept.
type Encryptor struct {
	w     io.Writer
	aead  cipher.AEAD
	nonce []byte
	buf   []byte
	n     int
	seq   uint64
	done  bool
}

// NewEncryptor starts an encrypted stream on w. The nonce prefix is random per
// archive, so two backups of identical content share no bytes.
func NewEncryptor(w io.Writer, key []byte) (*Encryptor, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	prefix := make([]byte, aead.NonceSize()-8)
	if _, err := rand.Read(prefix); err != nil {
		return nil, fmt.Errorf("generating a nonce: %w", err)
	}
	if _, err := w.Write(magic[:]); err != nil {
		return nil, err
	}
	if _, err := w.Write(prefix); err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	copy(nonce, prefix)
	return &Encryptor{w: w, aead: aead, nonce: nonce, buf: make([]byte, chunkSize)}, nil
}

func (e *Encryptor) Write(p []byte) (int, error) {
	if e.done {
		return 0, errors.New("backup: write after close")
	}
	written := 0
	for len(p) > 0 {
		n := copy(e.buf[e.n:], p)
		e.n += n
		p = p[n:]
		written += n
		if e.n == chunkSize {
			if err := e.seal(chunkMore); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

// Close seals whatever is buffered as the final chunk. It is always written,
// even for an empty stream, because the final chunk is how the reader knows it
// reached the end rather than the end of what someone left it.
func (e *Encryptor) Close() error {
	if e.done {
		return nil
	}
	if err := e.seal(chunkFinal); err != nil {
		return err
	}
	e.done = true
	return nil
}

func (e *Encryptor) seal(kind byte) error {
	binary.BigEndian.PutUint64(e.nonce[len(e.nonce)-8:], e.seq)
	e.seq++
	sealed := e.aead.Seal(nil, e.nonce, e.buf[:e.n], []byte{kind})
	var head [5]byte
	head[0] = kind
	binary.BigEndian.PutUint32(head[1:], uint32(len(sealed)))
	if _, err := e.w.Write(head[:]); err != nil {
		return err
	}
	if _, err := e.w.Write(sealed); err != nil {
		return err
	}
	e.n = 0
	return nil
}

// Decryptor reads a stream written by Encryptor. A chunk that does not
// authenticate, and a stream that ends before its final chunk, are both
// errors: silently returning the part that did verify would restore a
// truncated site over a working one.
type Decryptor struct {
	r     io.Reader
	aead  cipher.AEAD
	nonce []byte
	buf   []byte
	seq   uint64
	done  bool
}

// NewDecryptor opens an encrypted stream from r.
func NewDecryptor(r io.Reader, key []byte) (*Decryptor, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	var head [8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, fmt.Errorf("reading the archive header: %w", err)
	}
	if head != magic {
		return nil, errors.New("this is not a servlo backup, or it was written by a newer servlo")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(r, nonce[:aead.NonceSize()-8]); err != nil {
		return nil, fmt.Errorf("reading the archive header: %w", err)
	}
	return &Decryptor{r: r, aead: aead, nonce: nonce}, nil
}

func (d *Decryptor) Read(p []byte) (int, error) {
	for len(d.buf) == 0 {
		if d.done {
			return 0, io.EOF
		}
		if err := d.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.buf)
	d.buf = d.buf[n:]
	return n, nil
}

func (d *Decryptor) next() error {
	var head [5]byte
	if _, err := io.ReadFull(d.r, head[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return errors.New("the backup ends early: it was cut off in transfer or storage")
		}
		return err
	}
	kind := head[0]
	if kind != chunkMore && kind != chunkFinal {
		return errors.New("the backup is corrupt")
	}
	size := binary.BigEndian.Uint32(head[1:])
	if int(size) > chunkSize+d.aead.Overhead() {
		return errors.New("the backup is corrupt")
	}
	sealed := make([]byte, size)
	if _, err := io.ReadFull(d.r, sealed); err != nil {
		return errors.New("the backup ends early: it was cut off in transfer or storage")
	}
	binary.BigEndian.PutUint64(d.nonce[len(d.nonce)-8:], d.seq)
	d.seq++
	plain, err := d.aead.Open(nil, d.nonce, sealed, []byte{kind})
	if err != nil {
		return errors.New("the backup could not be opened: wrong key, or the archive has been altered")
	}
	d.buf = plain
	d.done = kind == chunkFinal
	return nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("a backup key is %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
