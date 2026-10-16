package mail

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

// fakeSMTP accepts one message and returns it.
func fakeSMTP(t *testing.T) (port string, got chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got = make(chan string, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { c.Write([]byte(s + "\r\n")) }
		say("220 fake")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					say("250 ok")
					got <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				say("250 ok")
			case cmd == "DATA":
				inData = true
				say("354 go")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	return p, got
}

func TestSend(t *testing.T) {
	port, got := fakeSMTP(t)
	c := Config{Host: "127.0.0.1", Port: port, From: "todo <noreply@example.com>"}
	if err := c.Send("a@b.co", "Halo\r\nBcc: evil@x.io", "baris1\nbaris2"); err != nil {
		t.Fatal(err)
	}
	msg := <-got
	if strings.Contains(msg, "\r\nBcc:") {
		t.Errorf("header injection: %q", msg)
	}
	if !strings.Contains(msg, "To: a@b.co") || !strings.Contains(msg, "baris1\r\nbaris2") {
		t.Errorf("unexpected message: %q", msg)
	}
}

func TestSendRefusesCredentialsWithoutTLS(t *testing.T) {
	port, _ := fakeSMTP(t)
	c := Config{Host: "127.0.0.1", Port: port, From: "x@y.z", User: "u", Pass: "p"}
	if err := c.Send("a@b.co", "s", "b"); err == nil {
		t.Error("sent credentials in clear text")
	}
}

func TestDisabled(t *testing.T) {
	if (Config{}).Enabled() || (Config{}).Send("a@b.co", "s", "b") == nil {
		t.Error("empty config must be disabled")
	}
}
