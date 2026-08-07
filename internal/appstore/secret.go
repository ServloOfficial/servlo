package appstore

import (
	"crypto/rand"
	"fmt"
)

// Generating the values a definition asks for.
//
// The alphabet is deliberately narrower than base64. These values are
// substituted into a config file the application then executes, so a quote, a
// backslash or a newline would end the string it lands in. Render refuses such
// a value, and generating one that Render would refuse is a bug waiting for a
// one-in-a-hundred install to find it, so the alphabet cannot produce one.
const secretAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" +
	"!#$%&()*+,-./:;<=>?@[]^_{|}~"

// GenerateSecrets returns a value for each secret the definition declares.
func (a App) GenerateSecrets() (map[string]string, error) {
	out := make(map[string]string, len(a.Secrets))
	for _, sec := range a.Secrets {
		v, err := randomString(sec.Length)
		if err != nil {
			return nil, fmt.Errorf("generating %q: %w", sec.Name, err)
		}
		out[sec.Name] = v
	}
	return out, nil
}

// randomString draws n characters from the alphabet without modulo bias: a
// byte landing outside the largest whole multiple of the alphabet is redrawn
// rather than folded, which would make the first few characters likelier.
func randomString(n int) (string, error) {
	const max = 256 - (256 % len(secretAlphabet))
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if int(b) >= max {
				continue
			}
			out = append(out, secretAlphabet[int(b)%len(secretAlphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}
