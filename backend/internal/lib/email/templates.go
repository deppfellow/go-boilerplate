package email

// Template is a string-based enum naming email templates.
type Template string

const (
	// TemplateWelcome corresponds to templates/emails/welcome.html
	TemplateWelcome Template = "welcome"
)
