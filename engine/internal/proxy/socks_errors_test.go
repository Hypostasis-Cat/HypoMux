package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestSOCKSConnectFailurePreservesErrorClass(t *testing.T) {
	for _, tc := range []struct {
		err   error
		reply byte
	}{
		{context.DeadlineExceeded, 6}, {syscall.ECONNREFUSED, 5},
		{syscall.Errno(10061), 5}, {syscall.ENETUNREACH, 3},
		{syscall.EHOSTUNREACH, 4}, {errors.New("no adapter available"), 1},
		{errors.Join(syscall.ECONNREFUSED, context.DeadlineExceeded), 1},
		{errors.Join(syscall.ECONNREFUSED, syscall.ECONNREFUSED), 5},
	} {
		wrapped := fmt.Errorf("Ethernet connect: %w", &net.OpError{Op: "dial", Err: tc.err})
		if got := socksConnectFailureReply(wrapped); got != tc.reply {
			t.Errorf("%v: got %d want %d", tc.err, got, tc.reply)
		}
	}
}

func TestConnectFailureLogKeepsCauseAndLimitsBursts(t *testing.T) {
	s := &Server{}
	var messages []string
	s.SetConnectFailureHandler(func(message string) { messages = append(messages, message) })
	for i := 0; i < 10; i++ {
		s.reportConnectFailure("aggregation", "192.0.2.1:443", errors.New("Ethernet: i/o timeout"), 6)
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "Ethernet: i/o timeout") || !strings.Contains(messages[0], "192.0.2.1:443") {
		t.Fatalf("unexpected diagnostic output: %v", messages)
	}
}
