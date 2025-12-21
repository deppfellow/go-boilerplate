package email

// SendWelcomeEmail sends a welcome email to a new user.
//
// It builds template data and calls SendEmail using the "welcome" template.
func (c *Client) SendWelcomeEmail(to, firstName string) error {
	// Data keys must match what your HTML template expects.
	data := map[string]string{
		"UserFirstName": firstName,
	}

	return c.SendEmail(
		to,
		"Welcome to Boilerplate!",
		TemplateWelcome,
		data,
	)
}
