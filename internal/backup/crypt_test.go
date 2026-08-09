package backup

import (
	"bytes"
	"crypto/rand"
	"io"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func roundTrip(t *testing.T, key []byte, plain []byte) []byte {
	t.Helper()
	var sealed bytes.Buffer
	w, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewDecryptor(bytes.NewReader(sealed.Bytes()), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The whole point: what goes in comes back out, at sizes either side of the
// chunk boundary, because a backup that only round-trips small inputs is a
// backup that fails on the first real site.
func TestEncryptDecrypt_RoundTripsAcrossChunkBoundaries(t *testing.T) {
	key := testKey(t)
	for _, size := range []int{0, 1, chunkSize - 1, chunkSize, chunkSize + 1, chunkSize*3 + 17} {
		plain := make([]byte, size)
		if _, err := rand.Read(plain); err != nil {
			t.Fatal(err)
		}
		if got := roundTrip(t, key, plain); !bytes.Equal(got, plain) {
			t.Errorf("size %d did not round-trip: got %d bytes back", size, len(got))
		}
	}
}

// A backup is ciphertext on someone else's disk. Two backups of identical
// content must not be byte-identical, or the storage tells an observer which
// sites are the same and when one stopped changing.
func TestEncrypt_TwoRunsOfTheSameInputDiffer(t *testing.T) {
	key := testKey(t)
	plain := []byte(strings.Repeat("the same site, twice", 500))

	var a, b bytes.Buffer
	for _, out := range []*bytes.Buffer{&a, &b} {
		w, err := NewEncryptor(out, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(plain); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two encryptions of the same content produced the same bytes")
	}
	if bytes.Contains(a.Bytes(), []byte("the same site")) {
		t.Error("the plaintext is in the output")
	}
}

// The wrong key must fail, and fail before handing back anything that looks
// like data.
func TestDecrypt_RefusesTheWrongKey(t *testing.T) {
	var sealed bytes.Buffer
	w, err := NewEncryptor(&sealed, testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("site data")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewDecryptor(bytes.NewReader(sealed.Bytes()), testKey(t))
	if err != nil {
		return // refusing at open is fine too
	}
	if got, err := io.ReadAll(r); err == nil {
		t.Errorf("the wrong key decrypted the backup, returning %q", got)
	}
}

// Tampering has to be caught. An archive an attacker can edit undetected is
// one that restores whatever they put in it.
func TestDecrypt_RefusesATamperedArchive(t *testing.T) {
	key := testKey(t)
	var sealed bytes.Buffer
	w, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("x"), 4096)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	bad := sealed.Bytes()
	bad[len(bad)-1] ^= 0xff
	r, err := NewDecryptor(bytes.NewReader(bad), key)
	if err != nil {
		return
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Error("a tampered archive decrypted without complaint")
	}
}

// Truncation is the failure a chunked format invites: every chunk is
// individually valid, so a cut-off archive reads as a complete short one
// unless the format says where the end is.
func TestDecrypt_RefusesATruncatedArchive(t *testing.T) {
	key := testKey(t)
	var sealed bytes.Buffer
	w, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("y"), chunkSize*2+10)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	cut := sealed.Bytes()[:chunkSize+100]
	r, err := NewDecryptor(bytes.NewReader(cut), key)
	if err != nil {
		return
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Error("a truncated archive read as a complete one")
	}
}

// Cutting the stream at a chunk boundary is the case the payload-truncation
// test above does not reach: every byte present is a whole, valid chunk, and
// the only thing wrong is that the final chunk never arrived. Reading that as a
// clean end would restore a site missing everything after the cut.
func TestDecrypt_RefusesAnArchiveCutAtAChunkBoundary(t *testing.T) {
	key := testKey(t)
	var sealed bytes.Buffer
	w, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("z"), chunkSize*2+10)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// magic + nonce prefix, then two whole sealed chunks. What is missing is
	// exactly the final chunk.
	boundary := len(magic) + 4 + 2*(5+chunkSize+16)
	r, err := NewDecryptor(bytes.NewReader(sealed.Bytes()[:boundary]), key)
	if err != nil {
		return
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Error("an archive cut at a chunk boundary read as a complete one")
	}
}

// The byte saying whether a chunk is the last one travels in the clear, so it
// has to be authenticated. Otherwise anyone who can edit the stored file turns
// the first chunk into the final one and the restore quietly brings back the
// first megabyte of the site.
func TestDecrypt_RefusesAChunkRelabelledAsTheLast(t *testing.T) {
	key := testKey(t)
	var sealed bytes.Buffer
	w, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte("q"), chunkSize*2)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	tampered := append([]byte(nil), sealed.Bytes()...)
	kindAt := len(magic) + 4 // the first chunk's header byte
	if tampered[kindAt] != chunkMore {
		t.Fatalf("expected a non-final first chunk, got kind %d", tampered[kindAt])
	}
	tampered[kindAt] = chunkFinal

	r, err := NewDecryptor(bytes.NewReader(tampered), key)
	if err != nil {
		return
	}
	if _, err := io.ReadAll(r); err == nil {
		t.Error("relabelling the first chunk as the last one truncated the archive undetected")
	}
}
