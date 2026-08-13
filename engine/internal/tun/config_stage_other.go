//go:build !windows

package tun

func stageTrustedConfig(config Config) (string, func(), error) {
	return config.ConfigPath, func() {}, nil
}

func PrepareTrustedConfigStorage() error { return nil }
