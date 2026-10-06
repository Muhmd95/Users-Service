package jobs

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/gomail.v2"
)

const smtpTimeout = 10 * time.Second

// gomail's DialAndSend has no context or SMTP command deadline. Keep its
// message encoding, but own the connection so shutdown can interrupt I/O.
func sendEmail(parent context.Context, dialer *gomail.Dialer, message *gomail.Message) (err error) {
	ctx, cancel := context.WithTimeout(parent, smtpTimeout)
	defer cancel()
	defer func() {
		if err != nil && ctx.Err() != nil {
			err = ctx.Err()
		}
	}()

	address := net.JoinHostPort(dialer.Host, strconv.Itoa(dialer.Port))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	tlsConfig := dialer.TLSConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{ServerName: dialer.Host}
	}
	var smtpConn net.Conn = conn
	if dialer.SSL {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return err
		}
		smtpConn = tlsConn
	}
	client, err := smtp.NewClient(smtpConn, dialer.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if dialer.LocalName != "" {
		if err := client.Hello(dialer.LocalName); err != nil {
			return err
		}
	}
	if !dialer.SSL {
		if supported, _ := client.Extension("STARTTLS"); supported {
			if err := client.StartTLS(tlsConfig); err != nil {
				return err
			}
		}
	}

	auth := dialer.Auth
	if auth == nil && dialer.Username != "" {
		if supported, mechanisms := client.Extension("AUTH"); supported {
			switch {
			case strings.Contains(mechanisms, "CRAM-MD5"):
				auth = smtp.CRAMMD5Auth(dialer.Username, dialer.Password)
			case strings.Contains(mechanisms, "LOGIN") && !strings.Contains(mechanisms, "PLAIN"):
				auth = &loginAuth{host: dialer.Host, username: dialer.Username, password: dialer.Password}
			default:
				auth = smtp.PlainAuth("", dialer.Username, dialer.Password, dialer.Host)
			}
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}

	sender := gomail.SendFunc(func(from string, to []string, message io.WriterTo) error {
		if err := client.Mail(from); err != nil {
			return err
		}
		for _, recipient := range to {
			if err := client.Rcpt(recipient); err != nil {
				return err
			}
		}
		writer, err := client.Data()
		if err != nil {
			return err
		}
		if _, err := message.WriteTo(writer); err != nil {
			return err
		}
		return writer.Close()
	})
	if err := gomail.Send(sender, message); err != nil {
		return err
	}
	// DATA was acknowledged: failure to end the session must not turn an
	// accepted email into a delivery error and invalidate its token.
	_ = client.Quit()
	return nil
}

// Retain support for servers advertising LOGIN without PLAIN.
type loginAuth struct {
	host, username, password string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server.Name != a.host {
		return "", nil, errors.New("SMTP authentication host mismatch")
	}
	if !server.TLS && server.Name != "localhost" && server.Name != "127.0.0.1" && server.Name != "::1" {
		return "", nil, errors.New("SMTP LOGIN requires TLS for a remote server")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(challenge))) {
	case "username:":
		return []byte(a.username), nil
	case "password:":
		return []byte(a.password), nil
	default:
		return nil, fmt.Errorf("unexpected SMTP LOGIN challenge: %q", challenge)
	}
}
