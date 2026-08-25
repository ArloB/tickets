package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveBaseName(t *testing.T) {
	got := archiveBaseName("v0.6.0", target{"linux", "amd64"})
	want := "tickets-v0.6.0-linux-amd64"
	if got != want {
		t.Errorf("archiveBaseName = %q, want %q", got, want)
	}
}

func TestBinaryNameAddsExeOnlyOnWindows(t *testing.T) {
	if got := (target{"windows", "amd64"}).binaryName(); got != "tickets.exe" {
		t.Errorf("windows binaryName = %q, want tickets.exe", got)
	}
	if got := (target{"linux", "amd64"}).binaryName(); got != "tickets" {
		t.Errorf("linux binaryName = %q, want tickets", got)
	}
}

func TestSHA256FileMatchesStdlib(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact.bin")
	content := []byte("release archive contents")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("sha256File = %q, want %q", got, want)
	}
}

// TestWriteSumsFormatIsSha256sumCompatible confirms the manifest is
// exactly the format `sha256sum -c SHA256SUMS` expects — two spaces
// between hash and filename, sorted by filename for a stable diff
// between releases regardless of build order.
func TestWriteSumsFormatIsSha256sumCompatible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SHA256SUMS")
	sums := map[string]string{
		"tickets-v1-windows-amd64.zip":  "bbbb",
		"tickets-v1-linux-amd64.tar.gz": "aaaa",
	}
	if err := writeSums(path, sums); err != nil {
		t.Fatalf("writeSums: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SHA256SUMS: %v", err)
	}
	want := "aaaa  tickets-v1-linux-amd64.tar.gz\nbbbb  tickets-v1-windows-amd64.zip\n"
	if string(data) != want {
		t.Errorf("SHA256SUMS content = %q, want %q", string(data), want)
	}
}

// TestWriteZipAndTarGzRoundTrip confirms both archive formats
// actually contain the staged payload under a single top-level
// directory, readable back with the standard library — the same
// contract a downloader's `unzip`/`tar xzf` relies on.
func TestWriteZipAndTarGzRoundTrip(t *testing.T) {
	stageDir := t.TempDir()
	root := "tickets-v1-test"
	payloadDir := filepath.Join(stageDir, root)
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		t.Fatalf("mkdir payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "tickets"), []byte("fake binary"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(payloadDir, "README.md"), []byte("readme"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	outDir := t.TempDir()

	zipPath := filepath.Join(outDir, "out.zip")
	if err := writeZip(zipPath, stageDir, root); err != nil {
		t.Fatalf("writeZip: %v", err)
	}
	assertZipContains(t, zipPath, []string{
		root + "/tickets", root + "/README.md",
	})

	tgzPath := filepath.Join(outDir, "out.tar.gz")
	if err := writeTarGz(tgzPath, stageDir, root); err != nil {
		t.Fatalf("writeTarGz: %v", err)
	}
	assertTarGzContains(t, tgzPath, []string{
		root + "/tickets", root + "/README.md",
	})
}

func assertZipContains(t *testing.T, path string, wantNames []string) {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = r.Close() }()

	got := map[string]bool{}
	for _, f := range r.File {
		got[f.Name] = true
	}
	for _, want := range wantNames {
		if !got[want] {
			t.Errorf("zip missing entry %q; got %v", want, got)
		}
	}
}

func assertTarGzContains(t *testing.T, path string, wantNames []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tar.gz: %v", err)
	}
	defer func() { _ = f.Close() }()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gr.Close() }()

	got := map[string]bool{}
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		got[hdr.Name] = true
	}
	for _, want := range wantNames {
		if !got[want] {
			t.Errorf("tar.gz missing entry %q; got %v", want, got)
		}
	}
}
