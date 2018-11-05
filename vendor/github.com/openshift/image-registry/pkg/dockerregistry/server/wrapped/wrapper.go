package wrapped

import "context"

// Wrapper is a user defined function that wraps methods to control their
// execution flow, contexts and error reporing.
type Wrapper func(ctx context.Context, funcname string, f func(ctx context.Context) error) error

// SimpleWrapper is a user defined function that wraps methods to control the
// execution flow and error reporing of methods. Unlike Wrapper, it wraps
// functions that don't use contexts, including low-level functions like
// (io.Reader).Read.
type SimpleWrapper func(funcname string, f func() error) error
