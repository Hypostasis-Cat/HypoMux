package services

import (
	"path/filepath"
	"sort"
	"strings"
)

type compatibilityDetection struct {
	Name   string `json:"name"`
	Path   string `json:"path,omitempty"`
	Source string `json:"source"`
}

type compatibilityPlan struct {
	ProcessNames []string                 `json:"process_names"`
	ProcessPaths []string                 `json:"process_paths"`
	Detected     []compatibilityDetection `json:"detected"`
}

// Names remain a conservative fallback for products that start after TUN.
// Active products are additionally pinned by their full executable path by
// the Windows detector, which avoids relying on a mutable process name alone.
var compatibilityProcessNames = []string{
	"qiyou.exe", "networkdaemon.exe", "qeetm.exe", "injhelper.exe", "injhelper64.exe", "lsphelper64.exe",
	"uu.exe", "uu_agent.exe", "uu_launcher.exe", "uu_ball.exe", "uu_neths_helper.exe", "uu_neth_helper.exe",
	"xunyou.exe", "xylauncher.exe", "xyprotectservice.exe", "xyservicelink.exe",
	"leigod.exe", "leigod_launcher.exe", "leishensdk.exe", "leigod-tool.exe",
	"clash.exe", "clash-meta.exe", "clash-win64.exe", "mihomo.exe", "mihomo-core.exe",
	"clash-verge.exe", "clash verge.exe", "clash-verge-rev.exe", "clash-verge-service.exe",
	"clash for windows.exe", "cfw.exe", "clashnyanpasu.exe", "flclash.exe",
	"v2rayn.exe", "v2ray.exe", "xray.exe", "nekoray.exe", "qv2ray.exe", "hiddify.exe", "hiddify-cli.exe",
	"shadowsocks.exe", "shadowsocksr.exe", "ss-local.exe", "surge.exe", "proxifier.exe",
}

func normalizedCompatibilityPlan(paths []string, detected []compatibilityDetection) compatibilityPlan {
	names := append([]string(nil), compatibilityProcessNames...)
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	seen := map[string]struct{}{}
	cleanPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		absolute, err := filepath.Abs(strings.TrimSpace(path))
		if err != nil || absolute == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(absolute))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleanPaths = append(cleanPaths, absolute)
	}
	sort.Slice(cleanPaths, func(i, j int) bool { return strings.ToLower(cleanPaths[i]) < strings.ToLower(cleanPaths[j]) })
	return compatibilityPlan{ProcessNames: names, ProcessPaths: cleanPaths, Detected: detected}
}
