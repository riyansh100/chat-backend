package common

import "errors"

// Fatal errors mean the client must be disconnected.
var ErrFatal = errors.New("fatal domain error")

// NonFatal errors mean the message should be ignored.
var ErrNonFatal = errors.New("non-fatal domain error")
