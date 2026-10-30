package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLineRolloutRejectsStoredPathRouteDespiteSpoofedFlag(t *testing.T) {
	ctx := context.Background()
	s, _, _, route := pathFixture(t)
	if err := s.SavePathRoute(ctx, route); err != nil {
		t.Fatal(err)
	}
	line, err := s.LineByID(ctx, route.ID)
	if err != nil {
		t.Fatal(err)
	}
	line.PathRoute = false // Old clients omit the read-only field.
	for _, remove := range []bool{false, true} {
		if _, err = s.PreviewLineRollout(ctx, line, remove, time.Minute); !errors.Is(err, ErrLineRolloutUnsupported) {
			t.Fatal("preview trusted client route flag", remove, err)
		}
		if _, err = s.PlanLineRollout(ctx, line, remove, time.Minute); !errors.Is(err, ErrLineRolloutUnsupported) {
			t.Fatal("plan trusted client route flag", remove, err)
		}
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM line_rollouts`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected route left a plan", count, err)
	}
}
