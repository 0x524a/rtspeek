package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
)

// message is one RTSP request or response, or one interleaved ($) frame.
type message struct {
	frame   []byte // set for an interleaved frame, forwarded as is
	start   string
	headers []string // raw header lines, order kept
	body    []byte
}

func (m *message) isResponse() bool { return strings.HasPrefix(m.start, "RTSP/") }

func (m *message) method() string {
	if m.isResponse() {
		return ""
	}
	f := strings.Fields(m.start)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// header returns the first value of the named header, case-insensitively.
func (m *message) header(name string) string {
	for _, h := range m.headers {
		if k, v, ok := strings.Cut(h, ":"); ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (m *message) setHeader(name, value string) {
	for i, h := range m.headers {
		if k, _, ok := strings.Cut(h, ":"); ok && strings.EqualFold(strings.TrimSpace(k), name) {
			m.headers[i] = name + ": " + value
			return
		}
	}
	m.headers = append(m.headers, name+": "+value)
}

func (m *message) bytes() []byte {
	if m.frame != nil {
		return m.frame
	}
	var b bytes.Buffer
	b.WriteString(m.start + "\r\n")
	for _, h := range m.headers {
		b.WriteString(h + "\r\n")
	}
	b.WriteString("\r\n")
	b.Write(m.body)
	return b.Bytes()
}

// readMessage reads the next RTSP message or interleaved frame.
func readMessage(br *bufio.Reader) (*message, error) {
	first, err := br.Peek(1)
	if err != nil {
		return nil, err
	}
	if first[0] == '$' {
		hdr := make([]byte, 4)
		if _, err := io.ReadFull(br, hdr); err != nil {
			return nil, err
		}
		n := int(hdr[2])<<8 | int(hdr[3])
		buf := make([]byte, 4+n)
		copy(buf, hdr)
		if _, err := io.ReadFull(br, buf[4:]); err != nil {
			return nil, err
		}
		return &message{frame: buf}, nil
	}
	tp := textproto.NewReader(br)
	start, err := tp.ReadLine()
	if err != nil {
		return nil, err
	}
	m := &message{start: start}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return nil, err
		}
		if line == "" {
			break
		}
		m.headers = append(m.headers, line)
	}
	if cl := m.header("Content-Length"); cl != "" {
		n, err := strconv.Atoi(cl)
		if err != nil || n < 0 || n > 1<<20 {
			return nil, fmt.Errorf("bad Content-Length %q", cl)
		}
		m.body = make([]byte, n)
		if _, err := io.ReadFull(br, m.body); err != nil {
			return nil, err
		}
	}
	return m, nil
}

var (
	clientPortRe = regexp.MustCompile(`client_port=(\d+)-(\d+)`)
	serverPortRe = regexp.MustCompile(`server_port=(\d+)-(\d+)`)
)

// udpTransport reports whether a Transport header asks for UDP unicast.
func udpTransport(t string) bool {
	return t != "" && !strings.Contains(t, "/TCP") && !strings.Contains(t, "interleaved") && clientPortRe.MatchString(t)
}

func portPair(re *regexp.Regexp, t string) (int, int, bool) {
	m := re.FindStringSubmatch(t)
	if m == nil {
		return 0, 0, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	return a, b, true
}

func replacePorts(re *regexp.Regexp, key, t string, a, b int) string {
	return re.ReplaceAllString(t, fmt.Sprintf("%s=%d-%d", key, a, b))
}
