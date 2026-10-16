// Package mail sends plain-text email over SMTP (STARTTLS on 587/25, implicit TLS on 465).
package mail

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

type Config struct {
	Host string
	Port string
	User string
	Pass string
	From string // e.g. "todo <noreply@example.com>" or bare address
}

// Enabled reports whether SMTP is configured; features that need email stay hidden otherwise.
func (c Config) Enabled() bool { return c.Host != "" && c.From != "" }

const dialTimeout = 10 * time.Second

func clean(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }

// bareAddr extracts the address from `Name <addr>` or returns s unchanged.
func bareAddr(s string) string {
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j > 0 {
			return s[i+1 : i+j]
		}
	}
	return s
}

func (c Config) Send(to, subject, body string) error {
	if !c.Enabled() {
		return errors.New("smtp not configured")
	}
	port := c.Port
	if port == "" {
		port = "587"
	}
	addr := net.JoinHostPort(c.Host, port)

	var conn net.Conn
	var err error
	if port == "465" {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: dialTimeout}, "tcp", addr, &tls.Config{ServerName: c.Host})
	} else {
		conn, err = net.DialTimeout("tcp", addr, dialTimeout)
	}
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()

	if port != "465" {
		if ok, _ := cl.Extension("STARTTLS"); ok {
			if err := cl.StartTLS(&tls.Config{ServerName: c.Host}); err != nil {
				return err
			}
		} else if c.User != "" {
			return errors.New("smtp server offers no STARTTLS; refusing to send credentials in clear text")
		}
	}
	if c.User != "" {
		if err := cl.Auth(smtp.PlainAuth("", c.User, c.Pass, c.Host)); err != nil {
			return err
		}
	}
	if err := cl.Mail(bareAddr(c.From)); err != nil {
		return err
	}
	if err := cl.Rcpt(clean(to)); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		clean(c.From), clean(to), clean(subject), time.Now().Format(time.RFC1123Z), strings.ReplaceAll(body, "\n", "\r\n"))
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}
