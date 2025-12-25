package service

// Auth service is responsible for authentication-related business logic.
//
// Tutor notes (02:48:06–02:50:35):
// - The first service in the boilerplate is AuthService.
// - Its job (initially) is to initialize Clerk with the secret key.
// - The secret key should come from config (loaded from env).
import (
	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/deppfellow/go-boilerplate/internal/server"
)

// AuthService encapsulates authentication functionality.
//
// It stores a pointer to *server.Server so it can access shared application dependencies
// (config, logger, etc.) if needed later.
//
// Note: Clerk SDK uses a global API key (clerk.SetKey), so initialization is a one-time setup.
type AuthService struct {
	server *server.Server
}

// NewAuthService initializes Clerk authentication and returns an AuthService.
//
// What this does:
// - Reads the Clerk Secret Key from config (which should be set via environment variables).
// - Calls clerk.SetKey(...) to configure the Clerk SDK globally for this process.
// - Returns a service instance that can later provide auth-related operations.
//
// Important:
//   - clerk.SetKey sets a global key for the entire application process.
//     Calling it multiple times is usually unnecessary; in this boilerplate it’s done once
//     during service initialization.
func NewAuthService(s *server.Server) *AuthService {
	// Initialize Clerk SDK with the secret key from config.
	// Tutor mentions you obtain this from Clerk dashboard and store it in env variables.
	clerk.SetKey(s.Config.Auth.SecretKey)

	return &AuthService{
		server: s,
	}
}
