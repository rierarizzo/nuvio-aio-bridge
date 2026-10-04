package metadata

import "context"

// Resolver fills title metadata for a Nuvio library entry.
type Resolver interface {
	Resolve(ctx context.Context, mediaType, imdbID string) (Meta, bool)
}

// Chain tries each resolver in order and returns the first hit. With a TMDB
// key first, artwork matches Nuvio's own; Cinemeta then covers anything TMDB
// does not know.
type Chain struct {
	resolvers []Resolver
}

// NewChain builds a resolver from the given sources, in priority order.
func NewChain(resolvers ...Resolver) Chain {
	return Chain{resolvers: resolvers}
}

// Resolve returns the first successful result.
func (c Chain) Resolve(ctx context.Context, mediaType, imdbID string) (Meta, bool) {
	for _, r := range c.resolvers {
		if r == nil {
			continue
		}
		if meta, ok := r.Resolve(ctx, mediaType, imdbID); ok {
			return meta, true
		}
	}
	return Meta{}, false
}
