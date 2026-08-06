package releasepack

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeterministicTarGzipAndZip(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "orm")
	readme := filepath.Join(root, "README.md")
	if err := os.WriteFile(binary, []byte("binary-content"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(readme, []byte("documentation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries := []archiveEntry{
		{Name: "orm", Path: binary, Mode: 0o755},
		{Name: "README.md", Path: readme, Mode: 0o644},
	}
	timestamp := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)

	for _, format := range []string{"tar", "zip"} {
		t.Run(format, func(t *testing.T) {
			first := filepath.Join(root, "first."+format)
			second := filepath.Join(root, "second."+format)
			var err error
			if format == "tar" {
				err = writeTarGzip(first, "release", entries, timestamp)
				if err == nil {
					err = writeTarGzip(second, "release", entries, timestamp)
				}
			} else {
				err = writeZip(first, "release", entries, timestamp)
				if err == nil {
					err = writeZip(second, "release", entries, timestamp)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			firstData, err := os.ReadFile(first)
			if err != nil {
				t.Fatal(err)
			}
			secondData, err := os.ReadFile(second)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(firstData, secondData) {
				t.Fatalf("%s archives are not byte-for-byte reproducible", format)
			}
		})
	}
}

func TestTarMetadataIsNormalized(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "input")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	archive := filepath.Join(root, "release.tar.gz")
	if err := writeTarGzip(archive, "release", []archiveEntry{{Name: "input", Path: path, Mode: 0o644}}, timestamp); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.ModTime != timestamp || header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "" {
			t.Fatalf("non-normalized tar header: %#v", header)
		}
	}
}

func TestZipMetadataIsNormalized(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "input")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	archive := filepath.Join(root, "release.zip")
	if err := writeZip(archive, "release", []archiveEntry{{Name: "input", Path: path, Mode: 0o644}}, timestamp); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 1 {
		t.Fatalf("zip files=%d", len(reader.File))
	}
	if !reader.File[0].Modified.Equal(timestamp) || reader.File[0].Mode().Perm() != 0o644 {
		t.Fatalf("non-normalized zip header: modified=%s mode=%o", reader.File[0].Modified, reader.File[0].Mode().Perm())
	}
}
