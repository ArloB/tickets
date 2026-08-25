// Command release cross-compiles the `tickets` binary for every
// Phase 6 Step 10 release target and packages each into an archive
// plus a SHA256SUMS manifest, invoked by `task release`
// (Taskfile.yml). It is a build-time tool, not part of the shipped
// product — ADR 0010's "cmd/tickets is the single entry point" is
// about what a Tickets install runs, not about dev/release tooling,
// the same distinction that already applies to Go's own `go` command
// not counting against a project's "one binary" claim.
//
// Pure Go cross-compilation (ADR 0003: no CGO anywhere in this
// module) means every target here builds with nothing beyond the Go
// toolchain already required for `task build` — no Windows toolchain
// needed to produce a Windows binary from Linux, no separate CI
// runners per platform.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// target is one release build: a GOOS/GOARCH pair. linux/amd64 and
// windows/amd64 are the two platforms product spec §15 names;
// linux/arm64 is included for the increasingly common arm64 Linux
// server/SBC case, at no extra cost since pure Go cross-compilation
// is free.
type target struct {
	goos   string
	goarch string
}

func (t target) String() string { return t.goos + "-" + t.goarch }

func (t target) binaryName() string {
	if t.goos == "windows" {
		return "tickets.exe"
	}
	return "tickets"
}

var targets = []target{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"windows", "amd64"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run() error {
	version := flag.String("version", "dev", "release version, embedded via -ldflags (matches task build's VERSION)")
	commit := flag.String("commit", "none", "short commit hash, embedded via -ldflags")
	date := flag.String("date", "unknown", "build date (RFC 3339 UTC), embedded via -ldflags")
	outDir := flag.String("out", "dist", "directory to write archives and SHA256SUMS into")
	flag.Parse()

	if err := os.RemoveAll(*outDir); err != nil {
		return fmt.Errorf("clear output dir %s: %w", *outDir, err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir %s: %w", *outDir, err)
	}

	ldflags := fmt.Sprintf(
		"-X github.com/ArloB/tickets/internal/buildinfo.Version=%s "+
			"-X github.com/ArloB/tickets/internal/buildinfo.Commit=%s "+
			"-X github.com/ArloB/tickets/internal/buildinfo.Date=%s",
		*version, *commit, *date)

	sums := make(map[string]string)
	for _, t := range targets {
		archivePath, err := buildAndPackage(t, *version, ldflags, *outDir)
		if err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
		sum, err := sha256File(archivePath)
		if err != nil {
			return fmt.Errorf("checksum %s: %w", archivePath, err)
		}
		sums[filepath.Base(archivePath)] = sum
	}

	if err := writeSums(filepath.Join(*outDir, "SHA256SUMS"), sums); err != nil {
		return fmt.Errorf("write SHA256SUMS: %w", err)
	}
	fmt.Printf("release: wrote %d archive(s) and SHA256SUMS to %s\n", len(sums), *outDir)
	return nil
}

// archiveBaseName is the shared stem for both the binary's staging
// directory and its final archive file, e.g.
// "tickets-v0.6.0-linux-amd64".
func archiveBaseName(version string, t target) string {
	return fmt.Sprintf("tickets-%s-%s-%s", version, t.goos, t.goarch)
}

// buildAndPackage cross-compiles one target and archives it (a .zip
// for Windows, matching what Windows users expect to double-click
// extract; a .tar.gz everywhere else, preserving the executable bit a
// zip's own permission bits handle inconsistently across
// implementations) into a single top-level directory containing the
// binary and README.md, the same shape most Go project release
// archives use. Returns the archive's path.
func buildAndPackage(t target, version, ldflags, outDir string) (string, error) {
	base := archiveBaseName(version, t)
	stageDir, err := os.MkdirTemp("", "tickets-release-*")
	if err != nil {
		return "", fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	payloadDir := filepath.Join(stageDir, base)
	if err := os.MkdirAll(payloadDir, 0o755); err != nil {
		return "", fmt.Errorf("create payload dir: %w", err)
	}

	binPath := filepath.Join(payloadDir, t.binaryName())
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", binPath, "./cmd/tickets")
	cmd.Env = append(os.Environ(),
		"GOOS="+t.goos, "GOARCH="+t.goarch, "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build: %w", err)
	}

	if err := copyFile("README.md", filepath.Join(payloadDir, "README.md")); err != nil {
		return "", fmt.Errorf("copy README.md: %w", err)
	}

	var archivePath string
	if t.goos == "windows" {
		archivePath = filepath.Join(outDir, base+".zip")
		err = writeZip(archivePath, stageDir, base)
	} else {
		archivePath = filepath.Join(outDir, base+".tar.gz")
		err = writeTarGz(archivePath, stageDir, base)
	}
	if err != nil {
		return "", err
	}
	return archivePath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

// writeZip archives everything under stageDir/rootName into a zip at
// archivePath, preserving rootName as the single top-level directory
// inside it.
func writeZip(archivePath, stageDir, rootName string) error {
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	zw := zip.NewWriter(f)
	defer func() { _ = zw.Close() }()

	root := filepath.Join(stageDir, rootName)
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stageDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		header.Method = zip.Deflate
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		_, err = io.Copy(w, src)
		return err
	})
}

// writeTarGz is writeZip's tar.gz counterpart, preserving the
// executable bit (unlike a zip's platform-inconsistent permission
// handling) since the payload is a Unix executable.
func writeTarGz(archivePath, stageDir, rootName string) error {
	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()
	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	root := filepath.Join(stageDir, rootName)
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stageDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		_, err = io.Copy(tw, src)
		return err
	})
}

// sha256File hashes an archive's bytes for SHA256SUMS — the same
// manifest.json checksum mechanism internal/backup.Backup uses for a
// data directory snapshot, applied here to a release artifact.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeSums renders sums in the sha256sum(1) binary-mode format
// ("<hex>  <filename>", two spaces, sorted by filename for a stable
// diff between releases) so `sha256sum -c SHA256SUMS` verifies a
// downloaded archive without any Tickets-specific tooling.
func writeSums(path string, sums map[string]string) error {
	names := make([]string, 0, len(sums))
	for name := range sums {
		names = append(names, name)
	}
	sort.Strings(names)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	for _, name := range names {
		if _, err := fmt.Fprintf(f, "%s  %s\n", sums[name], name); err != nil {
			return err
		}
	}
	return nil
}
