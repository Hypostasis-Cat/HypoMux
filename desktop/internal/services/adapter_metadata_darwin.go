//go:build darwin

package services

import (
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	darwinMetadataMu     sync.Mutex
	darwinMetadataAt     time.Time
	darwinMetadataCached map[int]adapterMetadata
	darwinDevicePattern  = regexp.MustCompile(`^\(Hardware Port: (.*), Device: ([^)]+)\)$`)
	darwinIfIndexPattern = regexp.MustCompile(`^if_index\s*:\s*([0-9]+)\s*\(([^)]+)\)`)
)

func adapterPlatformMetadata() map[int]adapterMetadata {
	darwinMetadataMu.Lock()
	defer darwinMetadataMu.Unlock()
	if time.Since(darwinMetadataAt) < 15*time.Second && darwinMetadataCached != nil {
		return cloneAdapterMetadata(darwinMetadataCached)
	}
	result := readDarwinAdapterMetadata()
	darwinMetadataCached = cloneAdapterMetadata(result)
	darwinMetadataAt = time.Now()
	return result
}

func readDarwinAdapterMetadata() map[int]adapterMetadata {
	result := make(map[int]adapterMetadata)
	serviceOutput, err := exec.Command(networkSetupPath, "-listnetworkserviceorder").Output()
	if err == nil {
		for _, service := range parseDarwinServiceOrder(string(serviceOutput)) {
			item, lookupErr := net.InterfaceByName(service.Device)
			if lookupErr != nil {
				continue
			}
			kind := "ethernet"
			label := strings.ToLower(service.Name + " " + service.HardwarePort)
			if strings.Contains(label, "wi-fi") || strings.Contains(label, "wifi") || strings.Contains(label, "airport") {
				kind = "wifi"
			}
			result[item.Index] = adapterMetadata{
				Metric: -1, AutoMetric: true, Name: service.Name,
				Description: service.HardwarePort + " (" + service.Device + ")", Kind: kind,
				Gateway: readDarwinGateway(service.Device),
			}
		}
	}
	dnsOutput, dnsErr := exec.Command("/usr/sbin/scutil", "--dns").Output()
	if dnsErr == nil {
		for index, servers := range parseDarwinDNS(string(dnsOutput)) {
			value := result[index]
			value.DNSServers = servers
			if value.Metric == 0 && value.Name == "" {
				value.Metric = -1
				value.AutoMetric = true
			}
			result[index] = value
		}
	}
	return result
}

type darwinNetworkService struct {
	Name         string
	HardwarePort string
	Device       string
}

func parseDarwinServiceOrder(output string) []darwinNetworkService {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	result := make([]darwinNetworkService, 0)
	currentName := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if close := strings.Index(line, ") "); strings.HasPrefix(line, "(") && close > 1 {
			currentName = strings.TrimSpace(line[close+2:])
			currentName = strings.TrimPrefix(currentName, "*")
			continue
		}
		match := darwinDevicePattern.FindStringSubmatch(line)
		if len(match) != 3 || currentName == "" {
			continue
		}
		device := strings.TrimSpace(match[2])
		if device == "" {
			continue
		}
		result = append(result, darwinNetworkService{
			Name: currentName, HardwarePort: strings.TrimSpace(match[1]), Device: device,
		})
		currentName = ""
	}
	return result
}

func parseDarwinDNS(output string) map[int][]string {
	result := make(map[int][]string)
	servers := make([]string, 0, 2)
	index := 0
	flush := func() {
		if index > 0 && len(servers) > 0 {
			seen := make(map[string]struct{}, len(result[index])+len(servers))
			for _, value := range result[index] {
				seen[value] = struct{}{}
			}
			for _, value := range servers {
				if _, exists := seen[value]; !exists {
					result[index] = append(result[index], value)
					seen[value] = struct{}{}
				}
			}
		}
		servers = nil
		index = 0
	}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "resolver #") || line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, "nameserver[") {
			if _, value, ok := strings.Cut(line, ":"); ok {
				if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
					servers = append(servers, ip.String())
				}
			}
			continue
		}
		if match := darwinIfIndexPattern.FindStringSubmatch(line); len(match) == 3 {
			index, _ = strconv.Atoi(match[1])
		}
	}
	flush()
	return result
}

func readDarwinGateway(device string) string {
	output, err := exec.Command("/sbin/route", "-n", "get", "-ifscope", device, "default").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "gateway" {
			gateway := strings.TrimSpace(value)
			if net.ParseIP(gateway) != nil {
				return gateway
			}
		}
	}
	return ""
}

func cloneAdapterMetadata(source map[int]adapterMetadata) map[int]adapterMetadata {
	result := make(map[int]adapterMetadata, len(source))
	for index, value := range source {
		value.DNSServers = append([]string(nil), value.DNSServers...)
		result[index] = value
	}
	return result
}
