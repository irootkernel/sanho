package fs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestSnapshotsSemanticallyEqualIgnoresArchiveMetadataAndPrefix(t *testing.T) {
	left := buildComparisonSnapshot(t, "", []comparisonEntry{
		{name: "guide/", typeflag: tar.TypeDir, mode: 0700},
		{name: "guide/readme.md", typeflag: tar.TypeReg, mode: 0644, data: "hello\n"},
		{name: "run.sh", typeflag: tar.TypeReg, mode: 0755, data: "#!/bin/sh\n"},
		{name: "current", typeflag: tar.TypeSymlink, mode: 0777, linkname: "guide/readme.md"},
	})
	right := buildComparisonSnapshot(t, "docs/", []comparisonEntry{
		{name: "guide/", typeflag: tar.TypeDir, mode: 0755},
		{name: "guide/readme.md", typeflag: tar.TypeReg, mode: 0664, data: "hello\n"},
		{name: "run.sh", typeflag: tar.TypeReg, mode: 0775, data: "#!/bin/sh\n"},
		{name: "current", typeflag: tar.TypeSymlink, mode: 0755, linkname: "guide/readme.md"},
	})
	equal, err := SnapshotsSemanticallyEqual(left, "", right, "docs")
	if err != nil || !equal {
		t.Fatalf("equal=%v err=%v", equal, err)
	}
}

func TestSnapshotsSemanticallyEqualDetectsContentModeAndSymlinkChanges(t *testing.T) {
	base := buildComparisonSnapshot(t, "", []comparisonEntry{
		{name: "readme.md", typeflag: tar.TypeReg, mode: 0644, data: "hello\n"},
		{name: "current", typeflag: tar.TypeSymlink, mode: 0777, linkname: "readme.md"},
	})
	for _, tc := range []struct {
		name  string
		entry []comparisonEntry
	}{
		{name: "content", entry: []comparisonEntry{{name: "readme.md", typeflag: tar.TypeReg, mode: 0644, data: "changed\n"}, {name: "current", typeflag: tar.TypeSymlink, mode: 0777, linkname: "readme.md"}}},
		{name: "mode", entry: []comparisonEntry{{name: "readme.md", typeflag: tar.TypeReg, mode: 0755, data: "hello\n"}, {name: "current", typeflag: tar.TypeSymlink, mode: 0777, linkname: "readme.md"}}},
		{name: "symlink", entry: []comparisonEntry{{name: "readme.md", typeflag: tar.TypeReg, mode: 0644, data: "hello\n"}, {name: "current", typeflag: tar.TypeSymlink, mode: 0777, linkname: "other.md"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := buildComparisonSnapshot(t, "", tc.entry)
			equal, err := SnapshotsSemanticallyEqual(base, "", other, "")
			if err != nil || equal {
				t.Fatalf("equal=%v err=%v", equal, err)
			}
		})
	}
}

type comparisonEntry struct {
	name     string
	typeflag byte
	mode     int64
	data     string
	linkname string
}

func buildComparisonSnapshot(t *testing.T, prefix string, entries []comparisonEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{
			Name:     prefix + entry.name,
			Typeflag: entry.typeflag,
			Mode:     entry.mode,
			Linkname: entry.linkname,
		}
		if entry.typeflag == tar.TypeReg {
			header.Size = int64(len(entry.data))
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.data != "" {
			if _, err := tarWriter.Write([]byte(entry.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
