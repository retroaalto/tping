//go:build linux

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
)

const (
	soEEOriginICMP      = 2
	icmpDestUnreachable = 3
	icmpPortUnreachable = 3
	icmpTimeExceeded    = 11
	sockExtendedErrLen  = 16
	sockaddrInet4Len    = 16
)

type hop struct {
	ttl      int
	ip       net.IP
	hostname string
	sent     int
	received int
	latest   time.Duration
	min      time.Duration
	max      time.Duration
	sum      time.Duration
}

func (h hop) lossPercent() float64 {
	if h.sent == 0 {
		return 0
	}
	return float64(h.sent-h.received) / float64(h.sent) * 100
}

func (h hop) average() time.Duration {
	if h.received == 0 {
		return 0
	}
	return h.sum / time.Duration(h.received)
}

var errTimeout = errors.New("timeout")

type traceResult struct {
	ttl     int
	ip      net.IP
	rtt     time.Duration
	reached bool
	err     error
	// fatal marks a local failure (socket, send, or receive) that will repeat
	// for every TTL, so the sweep stops instead of probing the rest.
	fatal bool
}

type traceConfig struct {
	maxHops int
	timeout time.Duration
}

func defaultTraceConfig() traceConfig {
	return traceConfig{
		maxHops: 30,
		timeout: 900 * time.Millisecond,
	}
}

func traceHost(host string, config traceConfig) ([]traceResult, error) {
	target, err := resolveIPv4(host)
	if err != nil {
		return nil, err
	}
	results := traceSweep(target, config)
	if last := results[len(results)-1]; last.fatal {
		return nil, last.err
	}
	return results, nil
}

func resolveIPv4(host string) (net.IP, error) {
	addresses, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, address := range addresses {
		if ipv4 := address.To4(); ipv4 != nil {
			return ipv4, nil
		}
	}
	return nil, fmt.Errorf("resolve %q: no IPv4 address", host)
}

func traceSweep(target net.IP, config traceConfig) []traceResult {
	results := make([]traceResult, 0, config.maxHops)
	for ttl := 1; ttl <= config.maxHops; ttl++ {
		result := traceProbe(target, config, ttl)
		results = append(results, result)
		if result.reached || result.fatal {
			break
		}
	}
	return results
}

func traceProbe(target net.IP, config traceConfig, ttl int) traceResult {
	result := traceResult{ttl: ttl}
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_ICMP)
	if err != nil {
		result.err = fmt.Errorf("open ICMP socket: %w", err)
		result.fatal = true
		return result
	}
	defer syscall.Close(fd)

	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_RECVERR, 1); err != nil {
		result.err = fmt.Errorf("enable IP_RECVERR: %w", err)
		result.fatal = true
		return result
	}
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_TTL, ttl); err != nil {
		result.err = fmt.Errorf("set TTL: %w", err)
		result.fatal = true
		return result
	}
	timeout := config.timeout
	if timeout <= 0 {
		timeout = 900 * time.Millisecond
	}
	timeval := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &timeval); err != nil {
		result.err = fmt.Errorf("set probe timeout: %w", err)
		result.fatal = true
		return result
	}

	destination := syscall.SockaddrInet4{}
	copy(destination.Addr[:], target)
	packet := makeICMPEcho(ttl)
	started := time.Now()
	if err := syscall.Sendto(fd, packet, 0, &destination); err != nil {
		result.err = fmt.Errorf("send probe: %w", err)
		result.fatal = true
		return result
	}

	reply := make([]byte, 64)
	_, replyAddress, receiveErr := syscall.Recvfrom(fd, reply, 0)
	if receiveErr == nil {
		// An echo reply means this TTL reached the destination. Linux ping
		// sockets omit the IPv4 header, so the first byte is the ICMP type.
		if len(reply) == 0 || reply[0] != 0 {
			result.err = errors.New("unexpected ICMP reply")
			return result
		}
		result.ip = target
		if address, ok := replyAddress.(*syscall.SockaddrInet4); ok {
			result.ip = net.IPv4(address.Addr[0], address.Addr[1], address.Addr[2], address.Addr[3])
		}
		result.rtt = time.Since(started)
		result.reached = true
		return result
	}
	if errors.Is(receiveErr, syscall.EAGAIN) || errors.Is(receiveErr, syscall.EINTR) {
		result.err = errTimeout
		return result
	}

	return readTraceError(fd, ttl, time.Since(started), receiveErr)
}

func makeICMPEcho(sequence int) []byte {
	packet := make([]byte, 8)
	packet[0] = 8 // ICMP Echo request
	binary.BigEndian.PutUint16(packet[4:6], uint16(sequence))
	binary.BigEndian.PutUint16(packet[2:4], internetChecksum(packet))
	return packet
}

func internetChecksum(packet []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(packet); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
	}
	if len(packet)%2 != 0 {
		sum += uint32(packet[len(packet)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// readTraceError reads the queued error after Recvfrom reports a network error.
// Linux documents msg_name as the original destination. The responding router is
// the sockaddr returned by SO_EE_OFFENDER immediately after sock_extended_err.
// When no ICMP error is queued, receiveErr was a local failure such as having no
// route to the network.
func readTraceError(fd, ttl int, rtt time.Duration, receiveErr error) traceResult {
	result := traceResult{ttl: ttl}
	packet := make([]byte, 64)
	oob := make([]byte, 128)
	_, oobLength, _, _, err := syscall.Recvmsg(fd, packet, oob, syscall.MSG_ERRQUEUE|syscall.MSG_DONTWAIT)
	if err != nil {
		result.err = fmt.Errorf("receive probe: %w", receiveErr)
		result.fatal = true
		return result
	}

	ip, reached, ok := parseTraceError(oob[:oobLength])
	if !ok {
		result.err = errors.New("unrecognized ICMP response")
		return result
	}
	result.ip = ip
	result.rtt = rtt
	result.reached = reached
	return result
}

func parseTraceError(oob []byte) (net.IP, bool, bool) {
	controlMessages, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, false, false
	}
	for _, controlMessage := range controlMessages {
		if controlMessage.Header.Level != syscall.IPPROTO_IP || controlMessage.Header.Type != syscall.IP_RECVERR {
			continue
		}
		if ip, reached, ok := parseTraceErrorData(controlMessage.Data); ok {
			return ip, reached, true
		}
	}
	return nil, false, false
}

func parseTraceErrorData(data []byte) (net.IP, bool, bool) {
	if len(data) < sockExtendedErrLen+sockaddrInet4Len || data[4] != soEEOriginICMP {
		return nil, false, false
	}
	icmpType := data[5]
	// Any destination-unreachable ends the path: port unreachable comes from
	// the target, other codes from a router that cannot forward further.
	reached := icmpType == icmpDestUnreachable
	if icmpType != icmpTimeExceeded && !reached {
		return nil, false, false
	}

	// struct sockaddr_in follows struct sock_extended_err. sin_addr is
	// four bytes after sin_family and sin_port.
	offender := data[sockExtendedErrLen:]
	ip := net.IPv4(offender[4], offender[5], offender[6], offender[7])
	if ip.IsUnspecified() {
		return nil, false, false
	}
	return ip, reached, true
}
