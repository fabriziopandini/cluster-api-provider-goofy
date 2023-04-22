package builder

import (
	cpredicate "github.com/fabriziopandini/cluster-api-provider-goofy/pkg/cloud/runtime/predicate"
)

// ForOption is some configuration that modifies options for a For request.
type ForOption interface {
	// ApplyToFor applies this configuration to the given for input.
	ApplyToFor(*ForInput)
}

// WatchesOption is some configuration that modifies options for a watches request.
type WatchesOption interface {
	// ApplyToWatches applies this configuration to the given watches options.
	ApplyToWatches(*WatchesInput)
}

// WithPredicates sets the given predicates list.
func WithPredicates(predicates ...cpredicate.Predicate) Predicates {
	return Predicates{
		predicates: predicates,
	}
}

// Predicates filters events before enqueuing the keys.
type Predicates struct {
	predicates []cpredicate.Predicate
}

// ApplyToFor applies this configuration to the given ForInput options.
func (w Predicates) ApplyToFor(opts *ForInput) {
	opts.predicates = w.predicates
}

// ApplyToWatches applies this configuration to the given WatchesInput options.
func (w Predicates) ApplyToWatches(opts *WatchesInput) {
	opts.predicates = w.predicates
}

var _ ForOption = &Predicates{}
var _ WatchesOption = &Predicates{}
