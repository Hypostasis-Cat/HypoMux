package wails

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var companionExportName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,100}\.(muxskin|md)$`)

func decodeCompanionExport(name, encoded string) ([]byte, error) {
	if !companionExportName.MatchString(name) || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid companion export filename")
	}
	const maxSize = 20 * 1024 * 1024
	if len(encoded) > base64.StdEncoding.EncodedLen(maxSize) {
		return nil, fmt.Errorf("companion export exceeds 20 MiB")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > maxSize {
		return nil, fmt.Errorf("invalid companion export data")
	}
	if strings.HasSuffix(name, ".md") && len(data) > 64*1024 {
		return nil, fmt.Errorf("creator guide exceeds 64 KiB")
	}
	return data, nil
}

// ExportCompanionFile only writes to a path explicitly chosen in the native dialog.
// It is a desktop UI binding and is not registered as an AI/MCP tool.
func (d *DesktopHost) ExportCompanionFile(name, encoded string) (bool, error) {
	data, err := decodeCompanionExport(name, encoded)
	if err != nil {
		return false, err
	}
	ext := filepath.Ext(name)
	path, err := d.app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title: "导出小 Mux 皮肤 / Export Mux skin", Filename: name, ButtonText: "保存 / Save", Window: d.window,
		Filters:              []application.FileFilter{{DisplayName: "Mux (*" + ext + ")", Pattern: "*" + ext}},
		CanCreateDirectories: true,
	}).PromptForSingleSelection()
	if err != nil || path == "" {
		return false, err
	}
	if !strings.EqualFold(filepath.Ext(path), ext) {
		return false, fmt.Errorf("请选择 %s 文件名 / Please use a %s filename", ext, ext)
	}
	if err := writeCompanionExport(path, data); err != nil {
		return false, err
	}
	return true, nil
}

func writeCompanionExport(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".mux-export-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporary, path)
}
