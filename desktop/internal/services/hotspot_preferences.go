package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// HotspotPreferences is separate from status/telemetry because it contains a secret.
func (s *EngineService) HotspotPreferences() (HotspotConfig, error) {
	config := HotspotConfig{SSID: "HypoMux", Band: "auto"}
	data, err := os.ReadFile(filepath.Join(settingsDirectory(), "hotspot.dat"))
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, errors.New("无法读取已保存的热点配置")
	}
	plain, err := protectHotspotPreferences(data, false)
	if err != nil {
		return config, errors.New("无法解密热点配置，请重新填写")
	}
	if json.Unmarshal(plain, &config) != nil || validateHotspotConfig(config) != nil {
		return HotspotConfig{SSID: "HypoMux", Band: "auto"}, errors.New("已保存的热点配置无效，请重新填写")
	}
	return config, nil
}

func saveHotspotPreferences(config HotspotConfig) error {
	plain, err := json.Marshal(config)
	if err != nil {
		return err
	}
	data, err := protectHotspotPreferences(plain, true)
	if err != nil {
		return errors.New("无法加密保存热点配置")
	}
	if atomicWriteFile(filepath.Join(settingsDirectory(), "hotspot.dat"), data, 0600) != nil {
		return errors.New("无法保存热点配置")
	}
	return nil
}
