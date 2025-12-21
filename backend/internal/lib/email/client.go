// Package email provides an email sending client.
//
// It currently uses Resend (resend-go) as the email provider and
// loads HTML templates from the filesystem to render email bodies.
package email

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/deppfellow/go-boilerplate/internal/config"
	"github.com/pkg/errors"
	"github.com/resend/resend-go/v2"
	"github.com/rs/zerolog"
)

// Client wraps the Resend client and a logger.
type Client struct {
	// client is the provider client used to send emails via API.
	client *resend.Client

	// logger is used for logging (not heavily used in this code snippet yet).
	logger *zerolog.Logger
}

// NewClient creates an email Client.
//
// It initializes a Resend client with the API key from config.
func NewClient(cfg *config.Config, logger *zerolog.Logger) *Client {
	return &Client{
		// Resend client initialized with API key.
		client: resend.NewClient(cfg.Integration.ResendAPIKey),
		logger: logger,
	}
}

// SendEmail sends an email with HTML rendered from a template file.
//
// Inputs:
//   - to: recipient email address
//   - subject: email subject line
//   - templateName: which template to use (e.g. "welcome")
//   - data: key/value pairs available inside the template
//
// Steps:
//   - Load template file from disk
//   - Execute template into a string buffer
//   - Call Resend API to send the email
func (c *Client) SendEmail(to, subject string, templateName Template, data map[string]string) error {
	// Build filesystem path to template HTML file.
	// Example: templates/emails/welcome.html
	tmplPath := fmt.Sprintf("%s/%s.html", "templates/emails", templateName)

	// ParseFiles loads the template file and returns a compiled template.
	// It can fail if file missing or template syntax invalid.
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		// pkg/errors.Wrapf adds context while preserving stack trace.
		return errors.Wrapf(err, "failed to parse email template %s", templateName)
	}

	// Execute template with `data` into a buffer (in-memory string builder).
	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		return errors.Wrapf(err, "failed to execute email template %s", templateName)
	}

	// Construct request for Resend API.
	params := &resend.SendEmailRequest{
		// "From" is the sender identity.
		// Resend may require a verified domain/address.
		From: fmt.Sprintf("%s <%s>", "Boilerplate", "onboarding@resend.dev"),

		// To can include multiple recipients; here it's just one.
		To: []string{to},

		Subject: subject,

		// Html is the email body.
		Html: body.String(),
	}

	// Send email through Resend.
	_, err = c.client.Emails.Send(params)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}
