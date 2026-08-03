package fs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

type snapshotEntry struct {
	typeflag byte
	mode     int64
	linkname string
	data     []byte
}

// SnapshotsSemanticallyEqual compares snapshot contents without depending on
// gzip or tar metadata such as timestamps and ownership. Prefixes are removed
// before comparison so a Git archive of docsDir can be compared with Sanho's
// docs-root-relative snapshots.
func SnapshotsSemanticallyEqual(left []byte, leftPrefix string, right []byte, rightPrefix string) (bool, error) {
	leftEntries, err := readSnapshotEntries(left, leftPrefix)
	if err != nil {
		return false, err
	}
	rightEntries, err := readSnapshotEntries(right, rightPrefix)
	if err != nil {
		return false, err
	}
	if len(leftEntries) != len(rightEntries) {
		return false, nil
	}
	for name, leftEntry := range leftEntries {
		rightEntry, ok := rightEntries[name]
		if !ok || leftEntry.typeflag != rightEntry.typeflag ||
			leftEntry.mode != rightEntry.mode || leftEntry.linkname != rightEntry.linkname ||
			!bytes.Equal(leftEntry.data, rightEntry.data) {
			return false, nil
		}
	}
	return true, nil
}

func readSnapshotEntries(snapshot []byte, prefix string) (map[string]snapshotEntry, error) {
	gzReader, err := gzip.NewReader(bytes.NewReader(snapshot))
	if err != nil {
		return nil, fmt.Errorf("open gzip snapshot: %w", err)
	}
	defer func() { _ = gzReader.Close() }()

	prefix = strings.Trim(path.Clean(strings.TrimPrefix(prefix, "./")), "/")
	if prefix == "." {
		prefix = ""
	}
	entries := make(map[string]snapshotEntry)
	tarReader := tar.NewReader(gzReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar snapshot: %w", err)
		}
		if header.Typeflag == tar.TypeXGlobalHeader || header.Typeflag == tar.TypeXHeader {
			continue
		}
		name, include, err := normalizedSnapshotPath(header.Name, prefix)
		if err != nil {
			return nil, err
		}
		if !include || name == "" {
			continue
		}
		if _, exists := entries[name]; exists {
			return nil, fmt.Errorf("duplicate snapshot path %q", name)
		}

		entry := snapshotEntry{typeflag: header.Typeflag}
		switch header.Typeflag {
		case tar.TypeReg, 0:
			entry.typeflag = tar.TypeReg
			entry.mode = normalizedGitFileMode(header.Mode)
			entry.data, err = io.ReadAll(tarReader)
			if err != nil {
				return nil, fmt.Errorf("read snapshot file %q: %w", name, err)
			}
		case tar.TypeSymlink:
			entry.mode = 0777
			entry.linkname = header.Linkname
		case tar.TypeDir:
			entry.mode = 0
		default:
			return nil, fmt.Errorf("unsupported snapshot entry type %d for %q", header.Typeflag, name)
		}
		entries[name] = entry
	}
	return entries, nil
}

func normalizedSnapshotPath(name, prefix string) (string, bool, error) {
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == "." || clean == "" {
		return "", false, nil
	}
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false, fmt.Errorf("invalid snapshot path %q", name)
	}
	if prefix != "" {
		if clean == prefix {
			return "", false, nil
		}
		if !strings.HasPrefix(clean, prefix+"/") {
			return "", false, fmt.Errorf("snapshot path %q is outside prefix %q", name, prefix)
		}
		clean = strings.TrimPrefix(clean, prefix+"/")
	}
	return clean, true, nil
}

func normalizedGitFileMode(mode int64) int64 {
	if mode&0111 != 0 {
		return 0755
	}
	return 0644
}
