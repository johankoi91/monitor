package identity

const DefaultBootstrapUsername = "admin"
const DefaultBootstrapPassword = "1111"

// Only the product's initial admin account permits the fixed four-character
// password. Registration and password changes keep their regular policy.
func validBootstrapCredentials(username, password string) bool {
	if !usernamePattern.MatchString(username) || len(password) > 72 {
		return false
	}
	return len(password) >= 6 || username == DefaultBootstrapUsername && password == DefaultBootstrapPassword
}
