package http

import "gitlab.com/boyang-hu/captain/internal/auth"

func newSessionID() string { return auth.Token(32) }
