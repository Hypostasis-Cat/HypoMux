// release-version validates release names and synchronizes build metadata.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hypostasis-Cat/HypoMux/desktop/internal/releaseversion"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	version := flag.String("version", "", "release version (defaults to desktop/VERSION)")
	tag := flag.String("tag", "", "release tag, including the v prefix")
	root := flag.String("root", ".", "desktop directory")
	write := flag.Bool("write", false, "synchronize version metadata")
	check := flag.Bool("check", false, "verify version metadata without changing files")
	notes := flag.Bool("notes", false, "require nonempty versioned release notes")
	output := flag.String("github-output", "", "append version/channel outputs to this file")
	flag.Parse()
	if flag.NArg() != 0 || (*write && *check) || (*tag != "" && *version != "") {
		return fmt.Errorf("invalid or conflicting arguments")
	}
	value := *version
	if *tag != "" {
		if !strings.HasPrefix(*tag, "v") {
			return fmt.Errorf("release tags must start with v")
		}
		value = strings.TrimPrefix(*tag, "v")
	}
	if value == "" {
		data, err := os.ReadFile(filepath.Join(*root, "VERSION"))
		if err != nil {
			return err
		}
		value = strings.TrimSpace(string(data))
	}
	v, err := releaseversion.Parse(value)
	if err != nil {
		return err
	}
	if *notes {
		path := filepath.Join(*root, "..", ".github", "release-notes", "v"+v.String()+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(data))) == 0 {
			return fmt.Errorf("release notes are empty: %s", path)
		}
	}
	if *write || *check {
		if err := releaseversion.SyncMetadata(*root, v, *check); err != nil {
			return err
		}
	}
	metadata := fmt.Sprintf("version=%s\nwindows_version=%s\nprerelease=%t\nmake_latest=%t\nchannel=%s\n", v, v.Windows(), v.Prerelease(), !v.Prerelease(), v.Channel())
	if *output != "" {
		file, err := os.OpenFile(*output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = file.WriteString(metadata)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	fmt.Print(metadata)
	return nil
}
