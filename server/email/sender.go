// Package email sends the one-off messages used by the auth flows
// (address verification, password reset) over SMTP, with a development-only
// log fallback for local runs without a mail server.
package email

import (
	"context"
	"time"

	"github.com/cheng-alvin/esplanade/server/config"
	"github.com/wneessen/go-mail"
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
	msg, err := buildMessage(s.from, to, subject, body)
	if err != nil {
		return err
	}
	client, err := mail.NewClient(s.host, s.clientOptions()...)
	if err != nil {
		return err
	}
	return client.DialAndSendWithContext(ctx, msg)
}

func (s *SMTPSender) clientOptions() []mail.Option {
	opts := []mail.Option{
		mail.WithPort(s.port),
		mail.WithTimeout(10 * time.Second),
		// Message bodies carry one-time verification/reset tokens, so a
		// plaintext fallback is never acceptable.
		mail.WithTLSPolicy(mail.TLSMandatory),
	}
	if s.username != "" {
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthPlain),
			mail.WithUsername(s.username),
			mail.WithPassword(s.password),
		)
	}
	if s.port == 465 {
		opts = append(opts, mail.WithSSL())
	}
	return opts
}

func buildMessage(from, to, subject, body string) (*mail.Msg, error) {
	msg := mail.NewMsg()
	if err := msg.From(from); err != nil {
		return nil, err
	}
	if err := msg.To(to); err != nil {
		return nil, err
	}
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, body)
	return msg, nil
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
