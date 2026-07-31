package proxy

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"strconv"
)

func (s *Server) handleSOCKS(reader *bufio.Reader, client net.Conn, session *connection) *Adapter {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 5 {
		return nil
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return nil
	}
	supportsNoAuth := false
	for _, method := range methods {
		if method == 0 {
			supportsNoAuth = true
			break
		}
	}
	if !supportsNoAuth {
		_, _ = client.Write([]byte{5, 0xff})
		return nil
	}
	if _, err := client.Write([]byte{5, 0}); err != nil {
		return nil
	}

	request := make([]byte, 4)
	if _, err := io.ReadFull(reader, request); err != nil || request[0] != 5 {
		return nil
	}
	host, ok := readSOCKSHost(reader, request[3])
	if !ok {
		writeSOCKSReply(client, 8)
		return nil
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return nil
	}
	port := int(binary.BigEndian.Uint16(portBytes))
	if request[1] == 3 {
		if session.channel == "" {
			writeSOCKSReply(client, 7)
			return nil
		}
		started, err := s.handleUDPAssociation(reader, client, session, host, port)
		if err != nil && !started {
			writeSOCKSReply(client, 1)
		}
		return nil
	}
	if request[1] != 1 {
		writeSOCKSReply(client, 7)
		return nil
	}
	target := net.JoinHostPort(host, strconv.Itoa(port))
	upstream, adapter, err := s.connect(session, target)
	if err != nil {
		writeSOCKSReply(client, 5)
		return nil
	}
	if !writeSOCKSReply(client, 0) {
		_ = upstream.Close()
		return nil
	}
	s.relay(reader, client, upstream, session)
	return &adapter
}

func readSOCKSHost(reader *bufio.Reader, addressType byte) (string, bool) {
	switch addressType {
	case 1:
		value := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(reader, value); err != nil {
			return "", false
		}
		return net.IP(value).String(), true
	case 3:
		length, err := reader.ReadByte()
		if err != nil || length == 0 {
			return "", false
		}
		value := make([]byte, int(length))
		if _, err := io.ReadFull(reader, value); err != nil {
			return "", false
		}
		return string(value), true
	case 4:
		value := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(reader, value); err != nil {
			return "", false
		}
		// IPv6 egress is a later migration slice; parse it so the caller gets a
		// normal connection failure instead of corrupting the SOCKS stream.
		return net.IP(value).String(), true
	default:
		return "", false
	}
}

func writeSOCKSReply(client net.Conn, reply byte) bool {
	_, err := client.Write([]byte{5, reply, 0, 1, 0, 0, 0, 0, 0, 0})
	return err == nil
}
