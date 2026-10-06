package jobs

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"gopkg.in/gomail.v2"
)

// Exercise real socket I/O, including a server that never sends its greeting
// or never acknowledges DATA. Neither stall was cancelable with DialAndSend.
func fakeSMTP(t *testing.T, stall, tlsMode string) (*gomail.Dialer, <-chan struct{}, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	address := listener.Addr().(*net.TCPAddr)
	dialer := gomail.NewDialer("127.0.0.1", address.Port, "", "")
	var serverTLS *tls.Config
	if tlsMode != "" {
		certServer := httptest.NewTLSServer(http.NotFoundHandler())
		serverTLS = certServer.TLS.Clone()
		roots := x509.NewCertPool()
		roots.AddCert(certServer.Certificate())
		certServer.Close()
		dialer.TLSConfig = &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"}
		dialer.SSL = tlsMode == "implicit"
	}
	stage := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- func() error {
			conn, err := listener.Accept()
			if err != nil {
				return err
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			if stall == "greeting" {
				close(stage)
				_, err := io.Copy(io.Discard, conn)
				return err
			}
			if tlsMode == "implicit" {
				tlsConn := tls.Server(conn, serverTLS)
				if err := tlsConn.Handshake(); err != nil {
					return err
				}
				conn = tlsConn
			}
			reader := bufio.NewReader(conn)
			if _, err := io.WriteString(conn, "220 localhost SMTP test\r\n"); err != nil {
				return err
			}
			tlsActive := tlsMode == "implicit"
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return err
				}
				var response string
				switch {
				case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"):
					if tlsMode == "starttls" && !tlsActive {
						response = "250-localhost\r\n250 STARTTLS\r\n"
					} else {
						response = "250 localhost\r\n"
					}
				case strings.HasPrefix(line, "STARTTLS"):
					if _, err := io.WriteString(conn, "220 Start TLS\r\n"); err != nil {
						return err
					}
					tlsConn := tls.Server(conn, serverTLS)
					if err := tlsConn.Handshake(); err != nil {
						return err
					}
					conn = tlsConn
					reader = bufio.NewReader(conn)
					tlsActive = true
					continue
				case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
					response = "250 OK\r\n"
				case strings.HasPrefix(line, "DATA"):
					if _, err := io.WriteString(conn, "354 Send message\r\n"); err != nil {
						return err
					}
					var body strings.Builder
					for {
						line, err := reader.ReadString('\n')
						if err != nil {
							return err
						}
						if line == ".\r\n" {
							break
						}
						body.WriteString(line)
					}
					if !strings.Contains(body.String(), "Subject: Shutdown test") || !strings.Contains(body.String(), "verification") {
						return errors.New("SMTP message did not contain the expected headers and body")
					}
					if stall != "quit" {
						close(stage)
					}
					if stall == "data" {
						_, err := io.Copy(io.Discard, conn)
						return err
					}
					response = "250 Message accepted\r\n"
				case strings.HasPrefix(line, "QUIT"):
					if stall == "quit" {
						close(stage)
						_, err := io.Copy(io.Discard, conn)
						return err
					}
					_, err := io.WriteString(conn, "221 Bye\r\n")
					return err
				default:
					return fmt.Errorf("unexpected SMTP command: %q", line)
				}
				if _, err := io.WriteString(conn, response); err != nil {
					return err
				}
			}
		}()
	}()
	return dialer, stage, done
}

func testMessage() *gomail.Message {
	message := gomail.NewMessage()
	message.SetHeader("From", "sender@example.com")
	message.SetHeader("To", "recipient@example.com")
	message.SetHeader("Subject", "Shutdown test")
	message.SetBody("text/plain", "verification")
	return message
}

func TestSMTPShutdownInterruptsStalledServer(t *testing.T) {
	for _, stall := range []string{"greeting", "data"} {
		t.Run(stall, func(t *testing.T) {
			dialer, stage, serverDone := fakeSMTP(t, stall, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- sendEmail(ctx, dialer, testMessage()) }()
			select {
			case <-stage:
			case <-time.After(time.Second):
				t.Fatal("SMTP operation did not reach the expected stage")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("send error = %v; want canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("SMTP send did not stop after cancellation")
			}
			select {
			case <-serverDone:
			case <-time.After(time.Second):
				t.Fatal("cancellation did not close the SMTP connection")
			}
		})
	}
}

func TestSMTPDeliverySupportsPlainImplicitTLSAndSTARTTLS(t *testing.T) {
	for _, mode := range []string{"", "implicit", "starttls"} {
		t.Run("mode="+mode, func(t *testing.T) {
			dialer, _, serverDone := fakeSMTP(t, "", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := sendEmail(ctx, dialer, testMessage()); err != nil {
				t.Fatal(err)
			}
			if err := <-serverDone; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSMTPCancellationAfterAcceptanceDoesNotReportDeliveryFailure(t *testing.T) {
	dialer, stage, serverDone := fakeSMTP(t, "quit", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- sendEmail(ctx, dialer, testMessage()) }()
	select {
	case <-stage:
	case <-time.After(time.Second):
		t.Fatal("SMTP server did not accept the message")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("accepted email reported as a delivery failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SMTP QUIT did not stop after cancellation")
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("SMTP connection remained open")
	}
}

func TestSMTPLoginAuthentication(t *testing.T) {
	auth := &loginAuth{host: "smtp.example.com", username: "user", password: "password"}
	server := &smtp.ServerInfo{Name: auth.host, Auth: []string{"LOGIN"}}
	if _, _, err := auth.Start(server); err == nil {
		t.Fatal("remote LOGIN accepted without TLS")
	}
	server.TLS = true
	mechanism, _, err := auth.Start(server)
	if err != nil || mechanism != "LOGIN" {
		t.Fatalf("TLS LOGIN initialization: %s, %v", mechanism, err)
	}
	for _, challenge := range []struct{ prompt, want string }{
		{"Username:", "user"}, {"Password:", "password"},
	} {
		response, err := auth.Next([]byte(challenge.prompt), true)
		if err != nil || string(response) != challenge.want {
			t.Fatalf("LOGIN challenge %q: %q, %v", challenge.prompt, response, err)
		}
	}
}
