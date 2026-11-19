package model

import "errors"

// ErrNotFound reports that a looked-up record does not exist. The store wraps
// it with what was looked up; callers test it with errors.Is.
var ErrNotFound = errors.New("not found")
