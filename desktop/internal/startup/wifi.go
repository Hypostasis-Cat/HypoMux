package startup

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"
)

type wifiInterface struct {
	id, name string
	state    uint32
}

type wifiProfile struct{ name, xml string }

type wifiSession interface {
	interfaces() ([]wifiInterface, error)
	profiles(string) ([]wifiProfile, error)
	connect(string, string) error
	close()
}

type wifiAttempt struct {
	at   time.Time
	next int
}

// WiFiConnector only requests connections on adapters selected for acceleration.
// It does not scan nearby networks, change profiles, enable radios, or write policy.
// The caller must still wait for DHCP/readiness: WlanConnect is asynchronous.
type WiFiConnector struct {
	open     func() (wifiSession, error)
	now      func() time.Time
	attempts map[string]wifiAttempt
}

func NewWiFiConnector() *WiFiConnector {
	return &WiFiConnector{open: openWiFiSession, now: time.Now, attempts: map[string]wifiAttempt{}}
}

func automaticWiFiProfile(payload string) bool {
	var profile struct {
		Mode string `xml:"connectionMode"`
		Type string `xml:"connectionType"`
	}
	return xml.Unmarshal([]byte(payload), &profile) == nil && profile.Mode == "auto" && profile.Type == "ESS"
}

// TryConnect is called serially during the bounded boot readiness loop.
// Profiles are tried in Windows preference order, allowing another saved network
// to be tried when the highest-priority one is unavailable at the current location.
func (c *WiFiConnector) TryConnect(ctx context.Context, selectedIDs []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(selectedIDs) == 0 {
		return nil
	}
	session, err := c.open()
	if err != nil {
		return err
	}
	defer session.close()
	interfaces, err := session.interfaces()
	if err != nil {
		return err
	}
	var failures []error
	for _, adapter := range interfaces {
		selected := false
		for _, id := range selectedIDs {
			selected = selected || strings.EqualFold(strings.TrimSpace(id), adapter.name)
		}
		if !selected {
			continue
		}
		// Do not replace a connected network or interrupt authentication in flight.
		if adapter.state == 0 {
			failures = append(failures, fmt.Errorf("Wi-Fi 网卡 %s 尚未就绪，请检查无线开关、飞行模式和 WLAN AutoConfig 服务", adapter.name))
			continue
		}
		if adapter.state != 4 {
			continue
		} // wlan_interface_state_disconnected
		last := c.attempts[adapter.id]
		if !last.at.IsZero() && c.now().Sub(last.at) < 20*time.Second {
			continue
		}
		profiles, err := session.profiles(adapter.id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		eligible := []string{}
		for _, profile := range profiles {
			if automaticWiFiProfile(profile.xml) {
				eligible = append(eligible, profile.name)
			}
		}
		if len(eligible) == 0 {
			failures = append(failures, fmt.Errorf("Wi-Fi 网卡 %s 没有允许自动连接的已保存网络，请先在 Windows 中连接 Wi-Fi 并勾选自动连接", adapter.name))
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		profile := eligible[last.next%len(eligible)]
		c.attempts[adapter.id] = wifiAttempt{at: c.now(), next: last.next + 1}
		if err := session.connect(adapter.id, profile); err != nil {
			failures = append(failures, fmt.Errorf("连接 Wi-Fi 网卡 %s：%w", adapter.name, err))
		}
	}
	return errors.Join(failures...)
}
