package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ArloB/tickets/internal/apiclient"
)

// runAttachment is `tickets attachment <subcommand>` — see runProject's
// doc comment for the client-mode convention this follows. Every
// subcommand that names an owner takes either a principal entity
// reference (e.g. ABC-1) or "comment:<id>" as its first positional
// argument (product spec §5.11: an attachment targets exactly one of
// the two) — a flag can't stand in for this because a leading flag
// would break the pop-positional-args-before-flags convention every
// other subcommand here follows (flag.FlagSet stops parsing at the
// first non-flag token).
func runAttachment(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("attachment: expected a subcommand (upload, path, list, get, versions, download, replace, delete)")
	}
	switch args[0] {
	case "upload":
		return runAttachmentUpload(args[1:])
	case "path":
		return runAttachmentPath(args[1:])
	case "list":
		return runAttachmentList(args[1:])
	case "get":
		return runAttachmentGet(args[1:])
	case "versions":
		return runAttachmentVersions(args[1:])
	case "download":
		return runAttachmentDownload(args[1:])
	case "replace":
		return runAttachmentReplace(args[1:])
	case "delete":
		return runAttachmentDelete(args[1:])
	default:
		return fmt.Errorf("attachment: unknown subcommand %q", args[0])
	}
}

// popOwner pops the leading positional argument as an owner — either
// a principal entity reference, or "comment:<id>" to target a comment
// instead (product spec §5.11: an attachment targets exactly one of
// the two) — before the rest of args is handed to fs.Parse, the same
// pop-positional-args-first convention popAttachmentID and every
// other subcommand here already follows (flag.FlagSet stops parsing
// at the first non-flag token, so a positional argument must never
// appear before the flags that follow it on the command line).
func popOwner(args []string, subcommand string) (ownerRef string, commentID int64, rest []string, err error) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return "", 0, nil, fmt.Errorf("attachment %s: expected a reference (or comment:<id>) as the first argument", subcommand)
	}
	if id, ok := strings.CutPrefix(args[0], "comment:"); ok {
		commentID, err = strconv.ParseInt(id, 10, 64)
		if err != nil {
			return "", 0, nil, fmt.Errorf("attachment %s: invalid comment id %q", subcommand, id)
		}
		return "", commentID, args[1:], nil
	}
	return args[0], 0, args[1:], nil
}

func runAttachmentUpload(args []string) error {
	ownerRef, commentID, rest, err := popOwner(args, "upload")
	if err != nil {
		return err
	}
	if len(rest) < 1 || strings.HasPrefix(rest[0], "-") {
		return fmt.Errorf("attachment upload: expected a file path as the second argument")
	}
	filePath := rest[0]

	fs, cfg, err := newClientFlagSet("attachment upload")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the attachment's title (required)")
	mediaType := fs.String("media-type", "", "MIME type (defaults to the file's own detected type if omitted)")
	if err := fs.Parse(rest[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if *title == "" {
		return fmt.Errorf("attachment upload: --title is required")
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("attachment upload: open %s: %w", filePath, err)
	}
	defer func() { _ = f.Close() }()

	a, err := cfg.newClient().UploadAttachment(context.Background(), ownerRef, commentID, *title, fileBaseName(filePath), *mediaType, f)
	if err != nil {
		return err
	}
	return printAttachment(cfg, a)
}

func runAttachmentPath(args []string) error {
	ownerRef, commentID, rest, err := popOwner(args, "path")
	if err != nil {
		return err
	}
	if len(rest) < 1 || strings.HasPrefix(rest[0], "-") {
		return fmt.Errorf("attachment path: expected a path value as the second argument")
	}
	pathValue := rest[0]

	fs, cfg, err := newClientFlagSet("attachment path")
	if err != nil {
		return err
	}
	title := fs.String("title", "", "the attachment's title (required)")
	mediaType := fs.String("media-type", "", "MIME type, if known")
	if err := fs.Parse(rest[1:]); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	if *title == "" {
		return fmt.Errorf("attachment path: --title is required")
	}

	a, err := cfg.newClient().AddPathAttachment(context.Background(), ownerRef, commentID, *title, pathValue, *mediaType)
	if err != nil {
		return err
	}
	return printAttachment(cfg, a)
}

func runAttachmentList(args []string) error {
	ownerRef, commentID, rest, err := popOwner(args, "list")
	if err != nil {
		return err
	}

	fs, cfg, err := newClientFlagSet("attachment list")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	page, err := cfg.newClient().ListAttachments(context.Background(), ownerRef, commentID)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Attachments))
	for i, a := range page.Attachments {
		rows[i] = []string{strconv.FormatInt(a.ID, 10), a.Kind, a.Title, strconv.FormatInt(a.CurrentVersion, 10), a.FileName}
	}
	return writeTable(os.Stdout, []string{"ID", "KIND", "TITLE", "VERSION", "FILE"}, rows)
}

func runAttachmentGet(args []string) error {
	id, rest, err := popAttachmentID(args, "get")
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("attachment get")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	a, err := cfg.newClient().GetAttachment(context.Background(), id)
	if err != nil {
		return err
	}
	return printAttachment(cfg, a)
}

func runAttachmentVersions(args []string) error {
	id, rest, err := popAttachmentID(args, "versions")
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("attachment versions")
	if err != nil {
		return err
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	page, err := cfg.newClient().ListAttachmentVersions(context.Background(), id)
	if err != nil {
		return err
	}
	if cfg.JSON {
		return writeJSON(os.Stdout, page)
	}
	rows := make([][]string, len(page.Versions))
	for i, v := range page.Versions {
		rows[i] = []string{strconv.FormatInt(v.Version, 10), v.Kind, v.FileName, v.UploadedBy}
	}
	return writeTable(os.Stdout, []string{"VERSION", "KIND", "FILE", "UPLOADED_BY"}, rows)
}

func runAttachmentDownload(args []string) error {
	id, rest, err := popAttachmentID(args, "download")
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("attachment download")
	if err != nil {
		return err
	}
	version := fs.Int64("version", 0, "download this archived version instead of the current one")
	output := fs.String("output", "", "write to this file instead of stdout")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}

	client := cfg.newClient()
	var dl apiclient.AttachmentDownload
	if *version > 0 {
		dl, err = client.DownloadAttachmentVersion(context.Background(), id, *version)
	} else {
		dl, err = client.DownloadAttachment(context.Background(), id)
	}
	if err != nil {
		return err
	}
	defer func() { _ = dl.Content.Close() }()

	out := os.Stdout
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return fmt.Errorf("attachment download: create %s: %w", *output, err)
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	if _, err := io.Copy(out, dl.Content); err != nil {
		return fmt.Errorf("attachment download: write output: %w", err)
	}
	return nil
}

func runAttachmentReplace(args []string) error {
	id, rest, err := popAttachmentID(args, "replace")
	if err != nil {
		return err
	}
	// An upload replacement's file path, if given, is a positional
	// argument popped here (before flags) the same way every other
	// leading positional in this file is — a --path replacement has
	// none, so this is skipped whenever the next token is itself a
	// flag.
	var filePath string
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		filePath = rest[0]
		rest = rest[1:]
	}

	fs, cfg, err := newClientFlagSet("attachment replace")
	if err != nil {
		return err
	}
	mediaType := fs.String("media-type", "", "MIME type (defaults to the file's own detected type if omitted)")
	pathValue := fs.String("path", "", "set the new version to a path reference instead of uploading a file")
	ifVersion := fs.Int64("if-version", 0, "the attachment's current version, from a prior get (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("attachment replace: --if-version is required")
	}

	client := cfg.newClient()
	var a apiclient.Attachment
	if *pathValue != "" {
		a, err = client.ReplacePathAttachment(context.Background(), id, *pathValue, *mediaType, *ifVersion)
	} else {
		if filePath == "" {
			return fmt.Errorf("attachment replace: expected a file path as the second argument, or --path for a path-reference version")
		}
		var f *os.File
		f, err = os.Open(filePath)
		if err != nil {
			return fmt.Errorf("attachment replace: open %s: %w", filePath, err)
		}
		defer func() { _ = f.Close() }()
		a, err = client.ReplaceUploadAttachment(context.Background(), id, fileBaseName(filePath), *mediaType, f, *ifVersion)
	}
	if err != nil {
		return err
	}
	return printAttachment(cfg, a)
}

func runAttachmentDelete(args []string) error {
	id, rest, err := popAttachmentID(args, "delete")
	if err != nil {
		return err
	}
	fs, cfg, err := newClientFlagSet("attachment delete")
	if err != nil {
		return err
	}
	ifVersion := fs.Int64("if-version", 0, "the attachment's current version, from a prior get (required)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if err := cfg.finish(); err != nil {
		return err
	}
	set := visitedFlags(fs)
	if !set["if-version"] {
		return fmt.Errorf("attachment delete: --if-version is required")
	}
	if err := cfg.newClient().DeleteAttachment(context.Background(), id, *ifVersion); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "deleted attachment %d\n", id)
	return nil
}

func popAttachmentID(args []string, subcommand string) (id int64, rest []string, err error) {
	if len(args) < 1 || strings.HasPrefix(args[0], "-") {
		return 0, nil, fmt.Errorf("attachment %s: expected an attachment id as the first argument", subcommand)
	}
	id, err = strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("attachment %s: invalid attachment id %q", subcommand, args[0])
	}
	return id, args[1:], nil
}

func fileBaseName(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i < 0 {
		return path
	}
	return path[i+1:]
}

// printAttachment renders one attachment: a JSON object in --json
// mode, a single-row table otherwise (mirroring runDecisionGet's own
// table shape for a single record).
func printAttachment(cfg *clientConfig, a apiclient.Attachment) error {
	if cfg.JSON {
		return writeJSON(os.Stdout, a)
	}
	return writeTable(os.Stdout, []string{"ID", "KIND", "TITLE", "VERSION", "FILE"},
		[][]string{{strconv.FormatInt(a.ID, 10), a.Kind, a.Title, strconv.FormatInt(a.CurrentVersion, 10), a.FileName}})
}
