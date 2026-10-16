package config_test

import (
	"testing"

	"github.com/talyvor/track/internal/config"
)

// B39.15: email switched on without a mail server is a boot failure, not mail that silently never leaves.
func TestEmailEnabledWithoutHostRefusesToBoot(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRACK_EMAIL_ENABLED", "true")
	t.Setenv("TRACK_SMTP_PORT", "465")
	t.Setenv("TRACK_EMAIL_FROM", "notifications@talyvor.com")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load succeeded with TRACK_EMAIL_ENABLED on and no TRACK_SMTP_HOST")
	}
	t.Setenv("TRACK_SMTP_HOST", "smtp.mx.cloudflare.net")
	c, err := config.Load()
	if err != nil || !c.EmailEnabled || c.SMTPPort != 465 {
		t.Fatalf("complete settings: %+v, %v", c, err)
	}
}
