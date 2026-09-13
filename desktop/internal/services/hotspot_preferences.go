package services

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
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

// SaveHotspotPreferences also works before aggregation starts. Serialize with
// lifecycle operations so the atomic-file temporary path has a single writer.
func (s *EngineService) SaveHotspotPreferences(config HotspotConfig) error {
	if err := validateHotspotConfig(config); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.acquireLifecycle(ctx); err != nil {
		return err
	}
	defer s.releaseLifecycle()
	s.mu.Lock()
	h, closing := s.hotspot, s.closing
	s.mu.Unlock()
	if closing {
		return errors.New("HypoMux 正在退出")
	}
	if h != nil {
		select {
		case <-h.done:
		default:
			return errors.New("请先关闭热点再修改配置")
		}
	}
	return saveHotspotPreferences(config)
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
