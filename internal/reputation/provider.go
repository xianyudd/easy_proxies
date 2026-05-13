package reputation

import "context"

// Provider normalizes third-party IP intelligence sources.
type Provider interface {
	Name() string
	Lookup(ctx context.Context, ip string) (*Result, error)
}
