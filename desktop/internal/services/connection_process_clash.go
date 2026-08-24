package services

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path"
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
	used := make([]bool, len(clash))
	for _, item := range core {
		targetHost, targetPort := splitConnectionEndpoint(item.Target)
		best := -1
		bestScore := -1
		bestDistance := time.Duration(1<<63 - 1)
		for index, candidate := range clash {
			if used[index] || (clashProcessName(candidate.Metadata) == "" && clashDomain(candidate.Metadata) == "") {
				continue
			}
			candidatePort := string(candidate.Metadata.DestinationPort)
			if targetPort != "" && candidatePort != "" && targetPort != candidatePort {
				continue
			}
			score := connectionTargetScore(targetHost, candidate.Metadata)
			if score == 0 {
				continue
			}
			distance := item.StartedAt.Sub(candidate.Start)
			if distance < 0 {
				distance = -distance
			}
			if distance > 8*time.Second {
				continue
			}
			if score > bestScore || (score == bestScore && distance < bestDistance) {
				best, bestScore, bestDistance = index, score, distance
			}
		}
		if best >= 0 {
			used[best] = true
			metadata := clash[best].Metadata
			result[item.ID] = clashConnectionDetails{
				Process: clashProcessName(metadata),
				Domain:  clashDomain(metadata),
			}
		}
	}
	return result
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
