package services

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

type clashPort string

func (p *clashPort) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*p = clashPort(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*p = clashPort(number.String())
	return nil
}

type clashConnectionMetadata struct {
	Network         string    `json:"network"`
	SourceIP        string    `json:"sourceIP"`
	SourcePort      clashPort `json:"sourcePort"`
	DestinationIP   string    `json:"destinationIP"`
	DestinationPort clashPort `json:"destinationPort"`
	Host            string    `json:"host"`
	Process         string    `json:"process"`
	ProcessPath     string    `json:"processPath"`
}

type clashConnection struct {
	Metadata clashConnectionMetadata `json:"metadata"`
	Start    time.Time               `json:"start"`
	Upload   int64                   `json:"upload"`
	Download int64                   `json:"download"`
}

type clashConnectionSnapshot struct {
	Connections []clashConnection `json:"connections"`
}

type clashConnectionDetails struct {
	Process string
	Domain  string
}

func fetchClashConnectionDetails(
	ctx context.Context,
	config clashAPIConfig,
	connections []connectionTelemetry,
) map[uint64]clashConnectionDetails {
	if strings.TrimSpace(config.Endpoint) == "" || strings.TrimSpace(config.Secret) == "" {
		return map[uint64]clashConnectionDetails{}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+config.Endpoint+"/connections", nil)
	if err != nil {
		return map[uint64]clashConnectionDetails{}
	}
	request.Header.Set("Authorization", "Bearer "+config.Secret)
	response, err := (&http.Client{Timeout: 650 * time.Millisecond}).Do(request)
	if err != nil {
		return map[uint64]clashConnectionDetails{}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return map[uint64]clashConnectionDetails{}
	}
	var snapshot clashConnectionSnapshot
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&snapshot); err != nil {
		return map[uint64]clashConnectionDetails{}
	}
	return matchClashConnections(connections, snapshot.Connections)
}

func matchClashConnections(core []connectionTelemetry, clash []clashConnection) map[uint64]clashConnectionDetails {
	result := make(map[uint64]clashConnectionDetails)
	type candidateMatch struct {
		coreIndex  int
		clashIndex int
		score      int
		distance   time.Duration
		byteDelta  uint64
	}
	matches := make([]candidateMatch, 0, len(core))
	for coreIndex, item := range core {
		targetHost, targetPort := splitConnectionEndpoint(item.Target)
		for clashIndex, candidate := range clash {
			if clashProcessName(candidate.Metadata) == "" && clashDomain(candidate.Metadata) == "" {
				continue
			}
			if !connectionNetworkMatches(item.Protocol, candidate.Metadata.Network) {
				continue
			}
			candidatePort := string(candidate.Metadata.DestinationPort)
			if targetPort == "" || candidatePort == "" || targetPort != candidatePort {
				continue
			}
			score := connectionTargetScore(targetHost, candidate.Metadata)
			distance := item.StartedAt.Sub(candidate.Start)
			if distance < 0 {
				distance = -distance
			}
			maxDistance := 8 * time.Second
			if score == 0 {
				// FakeIP and pre-resolved destinations intentionally differ from the
				// literal IP received by the aggregation core. Start time and port
				// provide a narrow fallback for restoring Clash metadata.
				score = 1
				maxDistance = 2 * time.Second
			}
			if distance > maxDistance {
				continue
			}
			matches = append(matches, candidateMatch{
				coreIndex: coreIndex, clashIndex: clashIndex, score: score,
				distance: distance, byteDelta: connectionByteDelta(item, candidate),
			})
		}
	}
	sort.SliceStable(matches, func(left, right int) bool {
		if matches[left].score != matches[right].score {
			return matches[left].score > matches[right].score
		}
		if matches[left].distance != matches[right].distance {
			return matches[left].distance < matches[right].distance
		}
		return matches[left].byteDelta < matches[right].byteDelta
	})
	usedCore := make([]bool, len(core))
	usedClash := make([]bool, len(clash))
	for _, match := range matches {
		if usedCore[match.coreIndex] || usedClash[match.clashIndex] {
			continue
		}
		usedCore[match.coreIndex] = true
		usedClash[match.clashIndex] = true
		metadata := clash[match.clashIndex].Metadata
		result[core[match.coreIndex].ID] = clashConnectionDetails{
			Process: clashProcessName(metadata),
			Domain:  clashDomain(metadata),
		}
	}
	return result
}

func connectionNetworkMatches(protocol string, network string) bool {
	network = strings.ToLower(strings.TrimSpace(network))
	if network == "" {
		return true
	}
	return strings.Contains(strings.ToLower(protocol), "udp") == (network == "udp")
}

func connectionByteDelta(core connectionTelemetry, clash clashConnection) uint64 {
	return absoluteInt64Delta(core.BytesUp, clash.Upload) + absoluteInt64Delta(core.BytesDown, clash.Download)
}

func absoluteInt64Delta(left int64, right int64) uint64 {
	if left >= right {
		return uint64(left - right)
	}
	return uint64(right - left)
}

func connectionTargetScore(host string, metadata clashConnectionMetadata) int {
	host = normalizeConnectionHost(host)
	if host == "" {
		return 0
	}
	if host == normalizeConnectionHost(metadata.Host) {
		return 3
	}
	if host == normalizeConnectionHost(metadata.DestinationIP) {
		return 2
	}
	return 0
}

func normalizeConnectionHost(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "[]"))
}

func clashDomain(metadata clashConnectionMetadata) string {
	value := strings.TrimSuffix(normalizeConnectionHost(metadata.Host), ".")
	if value == "" || net.ParseIP(value) != nil {
		return ""
	}
	return value
}

func clashProcessName(metadata clashConnectionMetadata) string {
	value := strings.TrimSpace(metadata.ProcessPath)
	if value == "" {
		value = strings.TrimSpace(metadata.Process)
	}
	if value == "" {
		return ""
	}
	name := path.Base(strings.ReplaceAll(value, `\`, "/"))
	if name == "." || name == "/" {
		return ""
	}
	return name
}
