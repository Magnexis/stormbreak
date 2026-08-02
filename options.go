package stormbreak

import (
	"context"
	"time"
)

// Option customizes a retry execution.
type Option func(*options)

type options struct {
	classifier Classifier
	hooks      Hooks
	random     func() float64
	wait       func(context.Context, time.Duration) error
}

// WithClassifier replaces the default retry classifier.
func WithClassifier(classifier Classifier) Option {
	return func(o *options) { o.classifier = classifier }
}

// WithHooks installs synchronous, nil-safe observability hooks.
func WithHooks(hooks Hooks) Option {
	return func(o *options) { o.hooks = hooks }
}

// WithRandomSource supplies values in [0, 1) for deterministic jitter.
// It is primarily useful in tests; the function must be safe for its caller's use.
func WithRandomSource(source func() float64) Option {
	return func(o *options) { o.random = source }
}
