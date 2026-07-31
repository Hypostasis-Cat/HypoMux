package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	iconPath := flag.String("icon", "", "ICO resource")
	manifestPath := flag.String("manifest", "", "application manifest")
	infoPath := flag.String("info", "", "version info JSON")
	outputPath := flag.String("out", "wails_windows_amd64.syso", "COFF resource output")
	architecture := flag.String("arch", "amd64", "amd64, arm64, or 386")
	flag.Parse()
	if err := generate(*iconPath, *manifestPath, *infoPath, *outputPath, *architecture); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(iconPath, manifestPath, infoPath, outputPath, architecture string) error {
	if iconPath == "" || manifestPath == "" {
		return fmt.Errorf("icon and manifest are required")
	}
	resources := winres.ResourceSet{}
	iconFile, err := os.Open(iconPath)
	if err != nil {
		return err
	}
	icon, err := winres.LoadICO(iconFile)
	closeErr := iconFile.Close()
	if err != nil {
		return fmt.Errorf("load icon: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	if err := resources.SetIcon(winres.RT_ICON, icon); err != nil {
		return err
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	manifestResource, err := winres.AppManifestFromXML(manifest)
	if err != nil {
		return err
	}
	resources.SetManifest(manifestResource)
	if infoPath != "" {
		data, readErr := os.ReadFile(infoPath)
		if readErr != nil {
			return readErr
		}
		var info version.Info
		if err := info.UnmarshalJSON(data); err != nil {
			return err
		}
		resources.SetVersionInfo(info)
	}
	architectures := map[string]winres.Arch{
		"amd64": winres.ArchAMD64,
		"arm64": winres.ArchARM64,
		"386":   winres.ArchI386,
	}
	target, ok := architectures[architecture]
	if !ok {
		return fmt.Errorf("unsupported architecture %q", architecture)
	}
	output, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	if err := resources.WriteObject(output, target); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
