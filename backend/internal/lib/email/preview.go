package email

// PreviewData contains sample template data for local preview/testing.
//
// It maps:
//
//	templateName -> (templateVariableName -> exampleValue)
//
// Example:
//
//	PreviewData["welcome"]["UserFirstName"] == "John"
var PreviewData = map[string]map[string]string{
	"welcome": {
		"UserFirstName": "John",
	},
}
