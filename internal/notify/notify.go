package notify

import (
	"errors"
	"fmt"
	"strings"

	"github.com/containrrr/shoutrrr"
	"github.com/containrrr/shoutrrr/pkg/types"
)

// SplitURLs parses a comma-separated list of Shoutrrr service URLs (trimmed, empty parts dropped).
func SplitURLs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Validate checks that each configured URL can be loaded as a Shoutrrr service.
func Validate(raw string) error {
	urls := SplitURLs(raw)
	if len(urls) == 0 {
		return nil
	}
	_, err := shoutrrr.CreateSender(urls...)
	return err
}

// Send delivers body to all configured services with the given title (e.g. Gotify, Slack, …).
func Send(raw, title, body string) error {
	urls := SplitURLs(raw)
	if len(urls) == 0 {
		return nil
	}
	r, err := shoutrrr.CreateSender(urls...)
	if err != nil {
		return err
	}
	params := make(types.Params)
	params.SetTitle(title)
	errs := r.Send(body, &params)
	return joinErrors(errs)
}

func joinErrors(errs []error) error {
	var parts []string
	for _, e := range errs {
		if e != nil {
			parts = append(parts, e.Error())
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return errors.New(strings.Join(parts, "; "))
}

// FormatContainerUpdated builds the notification body for a successful recreate.
func FormatContainerUpdated(container, image, oldID, newID string) string {
	return fmt.Sprintf("Container %q was recreated with image %s.\nOld id: %s\nNew id: %s",
		container, image, oldID, newID)
}
