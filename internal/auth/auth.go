package auth

import (
	"net/http"
	"strings"
)

type Auth struct {
	validKeys map[string]bool
}

func New(keysCSV string) *Auth {
	a := &Auth{
		validKeys: make(map[string]bool),
	}

	keys := strings.Split(keysCSV, ",")
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			a.validKeys[key] = true
		}
	}

	return a
}

func (a *Auth) Validate(r *http.Request) (valid bool, key string) {
	key = r.Header.Get("X-API-Key")
	if key == "" {
		return false, ""
	}

	return a.validKeys[key], key
}
