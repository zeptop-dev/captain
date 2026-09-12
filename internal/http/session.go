package http

import "github.com/zeptop-dev/captain/internal/auth"

func newSessionID() string { return auth.Token(32) }
