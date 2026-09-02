package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword derives a bcrypt digest. Only the digest is ever stored.
func HashPassword(password string) (string, error) {
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(digest), nil
}

// CheckPassword reports whether password matches the stored bcrypt digest.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
