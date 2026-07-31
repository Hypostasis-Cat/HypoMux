package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

var tunConnectivityURLs = []string{
	"http://www.msftconnecttest.com/connecttest.txt",
	"https://www.baidu.com/",
}

func probeTUNConnectivity(parent context.Context) (string, error) {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: -1,
		}).DialContext,
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("联网探测重定向过多")
			}
			return nil
		},
	}
	var lastError error
	for _, endpoint := range tunConnectivityURLs {
		request, err := http.NewRequestWithContext(parent, http.MethodGet, endpoint, nil)
		if err != nil {
			lastError = err
			continue
		}
		response, err := client.Do(request)
		if err != nil {
			lastError = err
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		_ = response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 500 {
			return fmt.Sprintf("%s -> HTTP %d", endpoint, response.StatusCode), nil
		}
		lastError = fmt.Errorf("%s -> HTTP %d", endpoint, response.StatusCode)
	}
	if lastError == nil {
		lastError = errors.New("没有可用的联网验证端点")
	}
	return "", lastError
}
