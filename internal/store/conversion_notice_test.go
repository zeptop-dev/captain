package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zeptop-dev/captain/internal/db"
	"github.com/zeptop-dev/captain/internal/domain"
)

func TestConversionNoticePersistenceAndRevision(t *testing.T) {
	ctx := context.Background()
	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "notice.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(db.Migrate(ctx, conn, "sqlite"))
	s := New(conn)
	_, err = conn.Exec(`UPDATE plan_access_conversion SET converted_at=123,inbound_count=13,plan_count=1 WHERE id=1`)
	must(err)
	before, err := s.AccessCatalog(ctx)
	must(err)
	if before.Conversion == nil || before.Conversion.Dismissed {
		t.Fatal(before.Conversion)
	}
	receipt := *before.Conversion
	must(s.DismissConversionNotice(ctx, receipt))
	must(s.DismissConversionNotice(ctx, receipt))
	after, err := s.AccessCatalog(ctx)
	must(err)
	if !after.Conversion.Dismissed || after.Revision != before.Revision {
		t.Fatal("presentation changed grant revision", before.Revision, after.Revision)
	}
	if !reflect.DeepEqual(before.Inbounds, after.Inbounds) || !reflect.DeepEqual(before.Plans, after.Plans) || !reflect.DeepEqual(before.Groups, after.Groups) {
		t.Fatal("permissions changed")
	}
	var at, inbounds, plans int
	must(conn.QueryRow(`SELECT converted_at,inbound_count,plan_count FROM plan_access_conversion WHERE id=1`).Scan(&at, &inbounds, &plans))
	if at != 123 || inbounds != 13 || plans != 1 {
		t.Fatal("audit receipt changed")
	}
	other := New(conn)
	again, err := other.AccessCatalog(ctx)
	must(err)
	if !again.Conversion.Dismissed {
		t.Fatal("dismissal not persistent")
	}
	receipt.At++
	if err = s.DismissConversionNotice(ctx, receipt); !errors.Is(err, ErrAccessConflict) {
		t.Fatal(err)
	}
	_, err = conn.Exec(`UPDATE plan_access_conversion SET converted_at=124 WHERE id=1`)
	must(err)
	changed, err := s.AccessCatalog(ctx)
	must(err)
	if changed.Conversion.Dismissed {
		t.Fatal("new receipt inherited old dismissal")
	}
	_, err = conn.Exec(`DELETE FROM plan_access_conversion`)
	must(err)
	empty, err := s.AccessCatalog(ctx)
	must(err)
	if empty.Conversion != nil {
		t.Fatal(empty.Conversion)
	}
	if err = s.DismissConversionNotice(ctx, receipt); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = s.DismissConversionNotice(ctx, AccessConversion{}); !errors.Is(err, ErrInvalidAccess) {
		t.Fatal(err)
	}
	// A site reset cannot retain a prior site's hidden-notice preference.
	staff := &domain.User{Email: "admin@example.com", PasswordHash: "test-hash", Role: domain.RoleAdmin, Status: "active"}
	must(s.CreateStaff(ctx, staff))
	if _, err = s.ResetSite(ctx, staff.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	must(conn.QueryRow(`SELECT count(*) FROM settings WHERE key=?`, conversionNoticeSetting).Scan(&count))
	if count != 0 {
		t.Fatal("dismissal survived reset")
	}
	empty, err = s.AccessCatalog(ctx)
	must(err)
	if empty.Conversion != nil {
		t.Fatal("reset restored old migration receipt")
	}

}
