package namespaces

// Context represents a referential position for a given namespace. We use the
// namespace and a search space to generate the candidate space.
type Context struct {
	// Namespace is the root name for the context. It is effectively the
	// default name for the context.
	//
	// It can also be thought of as the level of reference required to
	// disambiguate references from those generated from the search space.
	Namespace Name

	// Search provides a list of namespaces to combine with unresolved
	// references to build out qualified references to other objects.
	Search []Name
}

var (
	DefaultContext = Context{
		Namespace: "default",
	}
)

// ResolutionOptions controls the behavior of Context.Resolve.
type ResolutionOptions struct {
	// Match will be used to filter the candidates during resolution.
	Match func(name Name) bool

	// Qualify ensures that all references returned by resolve are qualifiable
	// outside the context.
	Qualify bool
}

// Resolve returns the most valid reference for the provided name based on the
// match function.  The least specific reference required should be returned.
func (ctx *Context) Resolve(name Name, opts ResolutionOptions) (Name, error) {
	candidates, err := ctx.Candidates(name)
	if err != nil {
		return "", err
	}

	for i, candidate := range candidates {
		if opts.Match != nil && !opts.Match(candidate) {
			continue
		}

		if !opts.Qualify && i == 0 {
			// if we hit the first candidate, we have encountered the local
			// version of the name, so we only return the partial form.
			return name, nil
		} else {
			return candidate, nil
		}
	}

	return "", ErrNameUnresolved
}

// Candidates expands the name in the search space into a set of candidates.
// The provided list of candidates may be referenceable from the context.
//
// The first returned entry is always the local reference.
func (ctx *Context) Candidates(name Name) ([]Name, error) {
	roots := append([]Name{ctx.Namespace}, ctx.Search...)

	var candidates []Name
	for _, root := range roots {
		candidate, err := name.Join(root)
		if err != nil {
			// maybe just return these?
			return nil, err
		}

		candidates = append(candidates, candidate)
	}

	return candidates, nil
}
