package services

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// singBoxTunDNSMode keeps the current DNS-policy contract when the bundled
// binary is later moved to 1.14+. 1.14 defaults TUN DNS handling to hijack,
// which would otherwise make the explicit "system" policy intercept DNS.
func singBoxTunDNSMode(executable, policy string) (string, error) {
	command := exec.Command(executable, "version")
	configureBackgroundCommand(command)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取 sing-box 版本失败：%w", err)
	}
	major, minor, err := parseSingBoxMajorMinor(string(output))
	if err != nil {
		return "", err
	}
	if major < 1 || (major == 1 && minor < 14) {
		return "", nil
	}
	if normalizeTunDNSPolicy(policy) == "system" {
		return "disabled", nil
	}
	return "hijack", nil
}

func parseSingBoxMajorMinor(output string) (int, int, error) {
	const marker = "sing-box version "
	index := strings.Index(strings.ToLower(output), marker)
	if index < 0 {
		return 0, 0, fmt.Errorf("无法识别 sing-box 版本")
	}
	version := strings.Fields(output[index+len(marker):])
	if len(version) == 0 {
		return 0, 0, fmt.Errorf("无法识别 sing-box 版本")
	}
	parts := strings.SplitN(strings.TrimPrefix(version[0], "v"), ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("无法识别 sing-box 版本：%s", version[0])
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return 0, 0, fmt.Errorf("无法识别 sing-box 版本：%s", version[0])
	}
	return major, minor, nil
}
