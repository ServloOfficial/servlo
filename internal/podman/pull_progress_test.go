package podman

import (
	"reflect"
	"testing"
)

func TestLineWriter_SplitsOnNewline(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}
	w.Write([]byte("Trying to pull foo...\nCopying blob sha256:abc done\n"))
	want := []string{"Trying to pull foo...", "Copying blob sha256:abc done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLineWriter_SplitsOnCarriageReturn(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}
	w.Write([]byte("Copying blob [=>  ] 10MB / 40MB\rCopying blob [====>] 20MB / 40MB\rCopying blob done\n"))
	want := []string{
		"Copying blob [=>  ] 10MB / 40MB",
		"Copying blob [====>] 20MB / 40MB",
		"Copying blob done",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLineWriter_BuffersPartialLine(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}
	w.Write([]byte("Copying blob"))
	if len(got) != 0 {
		t.Fatalf("expected no lines yet, got %v", got)
	}
	w.Write([]byte(" sha256:abc done\nWriting manifest"))
	want := []string{"Copying blob sha256:abc done"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after second write: got %v, want %v", got, want)
	}
	w.flush()
	want = append(want, "Writing manifest")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after flush: got %v, want %v", got, want)
	}
}

func TestLineWriter_SkipsEmptyLines(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}
	w.Write([]byte("\n\nTrying to pull\n\n"))
	want := []string{"Trying to pull"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLineWriter_MixedCRLF(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}
	w.Write([]byte("line one\r\nline two\r\n"))
	want := []string{"line one", "line two"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
