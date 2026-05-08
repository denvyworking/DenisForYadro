package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Source struct {
	Path    string
	Data    []byte
	Size    int64
	ModTime time.Time
}

func Load(path string) (Source, error) {
	cleaned := filepath.Clean(path)
	info, err := os.Stat(cleaned)
	if err != nil {
		return Source{}, fmt.Errorf("stat archive: %w", err)
	}

	data, err := os.ReadFile(cleaned)
	if err != nil {
		return Source{}, fmt.Errorf("read archive: %w", err)
	}

	source := Source{Path: cleaned, Data: data, Size: info.Size(), ModTime: info.ModTime()}
	lower := strings.ToLower(cleaned)

	switch {
	case strings.HasSuffix(lower, ".zip"):
		return readZip(source)
	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		return readTarGz(source)
	case strings.HasSuffix(lower, ".gz"):
		return readGzip(source)
	default:
		return source, nil
	}
}

func readZip(source Source) (Source, error) {
	readerAt := bytes.NewReader(source.Data)
	zipReader, err := zip.NewReader(readerAt, int64(len(source.Data)))
	if err != nil {
		return Source{}, fmt.Errorf("open zip: %w", err)
	}
	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return Source{}, fmt.Errorf("open zip entry: %w", err)
		}
		content, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return Source{}, fmt.Errorf("read zip entry: %w", readErr)
		}
		return Source{Path: source.Path + "::" + file.Name, Data: content, Size: int64(len(content)), ModTime: file.Modified}, nil
	}
	return Source{}, fmt.Errorf("zip archive has no files")
}

func readTarGz(source Source) (Source, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(source.Data))
	if err != nil {
		return Source{}, fmt.Errorf("open gzip: %w", err)
	}
	defer gzReader.Close()
	return readTar(source.Path, gzReader)
}

func readTar(path string, reader io.Reader) (Source, error) {
	tarReader := tar.NewReader(reader)
	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Source{}, fmt.Errorf("read tar: %w", err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		content, readErr := io.ReadAll(tarReader)
		if readErr != nil {
			return Source{}, fmt.Errorf("read tar entry: %w", readErr)
		}
		return Source{Path: path + "::" + hdr.Name, Data: content, Size: int64(len(content)), ModTime: hdr.ModTime}, nil
	}
	return Source{}, fmt.Errorf("tar archive has no files")
}

func readGzip(source Source) (Source, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(source.Data))
	if err != nil {
		return Source{}, fmt.Errorf("open gzip: %w", err)
	}
	defer gzReader.Close()
	content, err := io.ReadAll(gzReader)
	if err != nil {
		return Source{}, fmt.Errorf("read gzip: %w", err)
	}
	return Source{Path: source.Path + "::content", Data: content, Size: int64(len(content)), ModTime: source.ModTime}, nil
}
