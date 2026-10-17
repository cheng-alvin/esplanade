// Package email sends the one-off messages used by the auth flows
// (address verification, password reset) over SMTP, with a development-only
// log fallback for local runs without a mail server.
package email

import (
	"context"
	"crypto/tls"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/cheng-alvin/esplanade/server/config"
	"go.uber.org/zap"
)

// Sender dispatches a plain-text message to a single recipient.
type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// NewSender returns an SMTP sender when cfg.SMTPHost is set, otherwise a
// development log sender that logs the full body (so verification/reset links
// can be copied locally without an SMTP server).
func NewSender(cfg *config.Config, logger *zap.Logger) Sender {
	if cfg.SMTPHost == "" {
		return &LogSender{logger: logger}
	}
	return &SMTPSender{
		host:     cfg.SMTPHost,
		port:     cfg.SMTPPort,
		username: cfg.SMTPUsername,
		password: cfg.SMTPPassword,
		from:     cfg.SMTPFrom,
	}
}

type SMTPSender struct {
	host     string
	port     int
	username string
	password string
	from     string
}

func (s *SMTPSender) Send(ctx context.Context, to, subject, body string) error {
	addr := net.JoinHostPort(s.host, strconv.Itoa(s.port))
	msg := buildMessage(s.from, to, subject, body)

	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}

	// Port 465 speaks TLS from the first byte, so the STARTTLS-aware
	// smtp.SendMail cannot serve it; the session is driven manually.
	if s.port == 465 {
		return s.sendImplicitTLS(addr, auth, to, msg)
	}

	// stdlib SendMail has no context/dialer plumbing, so ctx goes unused
	// here; the 10s dialer bounds only the implicit-TLS path above.
	return smtp.SendMail(addr, auth, s.from, []string{to}, msg)
}

func (s *SMTPSender) sendImplicitTLS(addr string, auth smtp.Auth, to string, msg []byte) error {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.host})
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(s.from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\n")
	b.WriteString("To: ")
	b.WriteString(to)
	b.WriteString("\r\n")
	b.WriteString("Subject: ")
	b.WriteString(subject)
	b.WriteString("\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

// LogSender is a development-only stand-in for SMTP: it logs the message body
// so verification/reset links can be copied from the server log. The body
// carries one-time tokens, so it must never be selected in production.
type LogSender struct {
	logger *zap.Logger
}

func (s *LogSender) Send(_ context.Context, to, subject, body string) error {
	s.logger.Warn("email not sent (smtp not configured)",
		zap.String("to", to),
		zap.String("subject", subject),
		zap.String("body", body),
	)
	return nil
}
