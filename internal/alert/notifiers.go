package alert

import (
	"fmt"

	"gjallar/internal/config"
)

// BuildNotifiers instantiates every named notifier from config. Shoutrrr URLs
// are validated here, at startup, before any check runs.
func BuildNotifiers(alerts map[string]config.Alert) (map[string]Notifier, error) {
	out := make(map[string]Notifier, len(alerts))
	for name, a := range alerts {
		switch a.Type {
		case "", "shoutrrr":
			n, err := NewShoutrrr(a.URL)
			if err != nil {
				return nil, fmt.Errorf("alert %q: %w", name, err)
			}
			out[name] = n
		case "freemobile":
			out[name] = NewFreeMobile(a.User, a.Pass)
		default:
			return nil, fmt.Errorf("alert %q: unknown type %q", name, a.Type)
		}
	}
	return out, nil
}
