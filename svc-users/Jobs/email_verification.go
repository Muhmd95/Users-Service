package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/gomail.v2"

	"svc-users/external/redis"
	"svc-users/internal/users"
	"svc-users/util/logger"
)

type EmailWorker struct {
	jobsChan <-chan users.EmailJob
	rdb      *redis.Client
	dialer   *gomail.Dialer
	sender   string
}

func NewEmailWorker(jobsChan <-chan users.EmailJob, rdb *redis.Client, host string, port int, user, pass, sender string) *EmailWorker {
	dialer := gomail.NewDialer(host, port, user, pass)

	return &EmailWorker{
		jobsChan: jobsChan,
		rdb:      rdb,
		dialer:   dialer,
		sender:   sender,
	}
}

func (w *EmailWorker) Start(ctx context.Context) {
	log := logger.Ctx(ctx).With().Str("component", "email_worker").Logger()
	log.Info().Msg("Email worker started")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Email worker stopping...")
			return
		case job, ok := <-w.jobsChan:
			if !ok {
				log.Info().Msg("Jobs channel closed. Email worker stopping...")
				return
			}
			w.processJob(ctx, job, log)
		}
	}
}

func (w *EmailWorker) processJob(ctx context.Context, job users.EmailJob, log zerolog.Logger) {
	// Store token in Redis mapped to email (expires in 24 hours)
	err := w.rdb.Set(ctx, "email_verify:"+job.Token, job.Email, 24*time.Hour)
	if err != nil {
		log.Error().Err(err).Str("email", job.Email).Msg("Failed to store verification token in Redis")
		return
	}

	// Prepare email message
	m := gomail.NewMessage()
	m.SetHeader("From", w.sender)
	m.SetHeader("To", job.Email)
	m.SetHeader("Subject", "Verify your email address")

	verifyURL := fmt.Sprintf("http://localhost:8001/v1/users/verify-email?token=%s", job.Token)
	body := fmt.Sprintf(`
		<h1>Welcome to Ewallet!</h1>
		<p>Please click the link below to verify your email address:</p>
		<p><a href="%s">%s</a></p>
		<p>This link will expire in 24 hours.</p>
	`, verifyURL, verifyURL)
	m.SetBody("text/html", body)

	// Send email
	if err := w.dialer.DialAndSend(m); err != nil {
		log.Error().Err(err).Str("email", job.Email).Msg("Failed to send verification email")
		_ = w.rdb.Del(ctx, "email_verify:"+job.Token)
		return
	}

	log.Info().Str("email", job.Email).Msg("Successfully sent verification email to user")
}
