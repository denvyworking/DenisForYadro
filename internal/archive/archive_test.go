package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPlainFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(source.Data) != "hello" {
		t.Fatalf("unexpected data %q", string(source.Data))
	}
}

func TestLoadZip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zipWriter := zip.NewWriter(file)
	entry, err := zipWriter.Create("log.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("hello zip")); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	source, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source.Data, []byte("hello zip")) {
		t.Fatalf("unexpected data %q", string(source.Data))
	}
}
