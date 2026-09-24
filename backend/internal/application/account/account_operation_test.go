package account

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

func TestAccountOperationDetailKeepsRefreshDiagnostics(t *testing.T) {
	err := &provider.CredentialRefreshError{
		Status: 400, Code: "invalid_grant", Message: "refresh token expired",
		Response: `{"error":"invalid_grant"}`, Permanent: true,
	}
	detail := AccountOperationDetail(err)
	for _, want := range []string{"HTTP 400", "invalid_grant", "refresh token expired", `{"error":"invalid_grant"}`} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail %q missing %q", detail, want)
		}
	}
	permanent := fmt.Errorf("%w: invalid_grant", ErrCredentialRefreshPermanent)
	if got := AccountOperationDetail(permanent); !strings.Contains(got, "OAuth refresh token 已永久失效") || !strings.Contains(got, "invalid_grant") {
		t.Fatalf("permanent detail = %q", got)
	}
	if got := accountOperationStage("billing_sync", err); got != "credential_refresh" {
		t.Fatalf("billing stopped at credential stage = %q", got)
	}
	if got := accountOperationStage("billing_sync", errors.New("upstream billing returned 403")); got != "billing_sync" {
		t.Fatalf("billing stage = %q", got)
	}
}
