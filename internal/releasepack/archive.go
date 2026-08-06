package releasepack

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type archiveEntry struct {
	Name string
	Path string
	Mode fs.FileMode
}

func writeTarGzip(path, root string, entries []archiveEntry, timestamp time.Time) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	gzipWriter, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	gzipWriter.Header.ModTime = timestamp
	gzipWriter.Header.OS = 255
	gzipWriter.Header.Name = ""
	gzipWriter.Header.Comment = ""
	tarWriter := tar.NewWriter(gzipWriter)

	entries = sortedEntries(entries)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:       cleanArchiveName(root) + "/",
		Mode:       0o755,
		Typeflag:   tar.TypeDir,
		ModTime:    timestamp,
		AccessTime: time.Time{},
		ChangeTime: time.Time{},
	}); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := writeTarEntry(tarWriter, root, entry, timestamp); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	return file.Sync()
}

func writeTarEntry(writer *tar.Writer, root string, entry archiveEntry, timestamp time.Time) error {
	info, err := regularFileInfo(entry.Path)
	if err != nil {
		return err
	}
	name := cleanArchiveName(filepath.ToSlash(filepath.Join(root, entry.Name)))
	header := &tar.Header{
		Name:       name,
		Mode:       int64(entry.Mode.Perm()),
		Size:       info.Size(),
		Typeflag:   tar.TypeReg,
		ModTime:    timestamp,
		AccessTime: time.Time{},
		ChangeTime: time.Time{},
		Uid:        0,
		Gid:        0,
		Uname:      "",
		Gname:      "",
		Format:     tar.FormatPAX,
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	file, err := os.Open(entry.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := io.Copy(writer, file); err != nil {
		return err
	}
	return nil
}

func writeZip(path, root string, entries []archiveEntry, timestamp time.Time) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	writer := zip.NewWriter(file)
	entries = sortedEntries(entries)
	for _, entry := range entries {
		if err := writeZipEntry(writer, root, entry, timestamp); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return file.Sync()
}

func writeZipEntry(writer *zip.Writer, root string, entry archiveEntry, timestamp time.Time) error {
	if _, err := regularFileInfo(entry.Path); err != nil {
		return err
	}
	header := &zip.FileHeader{
		Name:   cleanArchiveName(filepath.ToSlash(filepath.Join(root, entry.Name))),
		Method: zip.Deflate,
	}
	header.SetMode(entry.Mode)
	header.SetModTime(zipTimestamp(timestamp))
	destination, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	source, err := os.Open(entry.Path)
	if err != nil {
		return err
	}
	defer source.Close()
	_, err = io.Copy(destination, source)
	return err
}

func regularFileInfo(path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("releasepack: inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("releasepack: %s is not a regular file", path)
	}
	return info, nil
}

func sortedEntries(entries []archiveEntry) []archiveEntry {
	result := append([]archiveEntry(nil), entries...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func cleanArchiveName(name string) string {
	name = strings.TrimPrefix(filepath.ToSlash(name), "/")
	name = filepath.ToSlash(filepath.Clean(name))
	if name == "." || name == ".." || strings.HasPrefix(name, "../") {
		panic("releasepack: invalid archive name " + name)
	}
	return name
}

func zipTimestamp(timestamp time.Time) time.Time {
	minimum := time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	if timestamp.Before(minimum) {
		return minimum
	}
	return timestamp.UTC()
}
