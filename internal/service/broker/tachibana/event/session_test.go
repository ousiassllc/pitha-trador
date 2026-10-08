package event_test

import (
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/session"
)

// newSession is the login manager over the rig's client (the production
// wiring: ST p_errno=2 reaches it through Client.ReportSessionLost).
func newSession(t *testing.T, r *rig) *session.Session {
	t.Helper()
	return session.New(session.Config{
		Client: r.c, AuthID: "AUTHID-SECRET-1234", KeyPath: r.fb.KeyFile(), ReauthTime: config.DefaultTachibanaReauthTime,
		APIVersion: "e_api_v4r10", Clock: r.clk,
	})
}
