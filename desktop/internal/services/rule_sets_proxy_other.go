//go:build !windows

package services

import "net/url"

func systemRuleSetProxy() *url.URL { return nil }
