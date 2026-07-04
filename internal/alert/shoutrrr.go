package alert

import (
	"context"
	"errors"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// Shoutrrr sends notifications through any service shoutrrr supports
// (telegram, smtp, ntfy, discord, slack, webhooks, ...), configured by URL.
type Shoutrrr struct {
	sender *router.ServiceRouter
}

// NewShoutrrr validates the URL at startup, before any check runs.
func NewShoutrrr(url string) (*Shoutrrr, error) {
	sender, err := shoutrrr.CreateSender(url)
	if err != nil {
		return nil, err
	}
	return &Shoutrrr{sender: sender}, nil
}

func (s *Shoutrrr) Send(ctx context.Context, title, message string) error {
	errs := s.sender.Send(message, &types.Params{"title": title})
	return errors.Join(errs...)
}
