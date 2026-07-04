package alert

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

const freeMobileURL = "https://smsapi.free-mobile.fr/sendmsg"

// FreeMobile sends an SMS through the French Free Mobile subscriber API.
type FreeMobile struct {
	User    string
	Pass    string
	BaseURL string // overridable for tests; defaults to the Free Mobile API
	Client  *http.Client
}

func NewFreeMobile(user, pass string) *FreeMobile {
	return &FreeMobile{User: user, Pass: pass, BaseURL: freeMobileURL, Client: &http.Client{}}
}

func (f *FreeMobile) Send(ctx context.Context, title, message string) error {
	q := url.Values{}
	q.Set("user", f.User)
	q.Set("pass", f.Pass)
	q.Set("msg", title+"\n"+message)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.BaseURL+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusBadRequest:
		return fmt.Errorf("free mobile: missing parameter")
	case http.StatusPaymentRequired:
		return fmt.Errorf("free mobile: too many messages, quota exceeded")
	case http.StatusForbidden:
		return fmt.Errorf("free mobile: service not enabled or wrong credentials")
	case http.StatusInternalServerError:
		return fmt.Errorf("free mobile: server error")
	default:
		return fmt.Errorf("free mobile: unexpected status %d", resp.StatusCode)
	}
}
