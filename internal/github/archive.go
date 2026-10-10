package github

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
)

// maxFileBytes caps a single extracted file as a guard against decompression
// bombs. Posts and resume/settings JSON are tiny; images are the only large
// files and 25 MiB is far above anything we expect.
const maxFileBytes = 25 << 20

// ArchiveFile is one regular file from a repo tarball, with the top-level
// "<owner>-<repo>-<sha>/" wrapper directory stripped from its path.
type ArchiveFile struct {
	Path string // forward-slash, repo-relative
	Data []byte
}

// ExtractTarGz reads a gzipped tar stream (as produced by GitHub's tarball
// endpoint) and returns every regular file. The single leading path component
// GitHub adds is stripped. Directories, symlinks, and other non-regular
// entries are skipped.
func ExtractTarGz(r io.Reader) ([]ArchiveFile, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var files []ArchiveFile
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		rel := stripTopLevel(hdr.Name)
		if rel == "" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxFileBytes+1))
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", hdr.Name, err)
		}
		if len(data) > maxFileBytes {
			return nil, fmt.Errorf("file %q exceeds %d bytes", rel, maxFileBytes)
		}
		files = append(files, ArchiveFile{Path: rel, Data: data})
	}
	return files, nil
}

// stripTopLevel drops the first path segment (GitHub's "<owner>-<repo>-<sha>/"
// wrapper) and returns a clean, forward-slash relative path. It returns "" for
// the wrapper directory entry itself.
func stripTopLevel(name string) string {
	name = strings.TrimPrefix(path.Clean(name), "/")
	i := strings.IndexByte(name, '/')
	if i < 0 {
		return ""
	}
	return name[i+1:]
}
