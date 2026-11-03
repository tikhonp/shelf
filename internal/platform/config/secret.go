package config

import (
	"fmt"
	"log/slog"
)

// Secret is a token or password from the environment. It prints, logs and
// marshals as a placeholder, so a stray log line or API answer can't leak
// it; Reveal is the only way to the value.
type Secret string

const redacted = "[redacted]"

// Reveal returns the value, for the one place that sends it on.
func (s Secret) Reveal() string { return string(s) }

func (s Secret) String() string { return redacted }

// Format covers the verbs that skip String, such as %#v and %q.
func (s Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }
