package tools

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func tarOf(t *testing.T, name, body string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	if err := w.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func TestUntarWritesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := untar(tarOf(t, "src/A.java", "class A {}"), dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "src", "A.java"))
	if err != nil || string(got) != "class A {}" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestUntarRefusesPathsOutsideTheExport(t *testing.T) {
	dir := t.TempDir()
	if err := untar(tarOf(t, "../evil.txt", "x"), dir); err == nil {
		t.Fatal("a path outside the export must be refused")
	}
}
