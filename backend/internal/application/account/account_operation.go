package account

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chenyme/grok2api/backend/internal/infra/provider"
)

// AccountOperationFailure is one account's credential-refresh or quota-sync
// failure, safe to show in the admin UI and server logs.
type AccountOperationFailure struct {
	AccountID  uint64
	Name       string
	Email      string
	Stage      string
	HTTPStatus int
	Code       string
	Detail     string
}

// AccountOperationFailureObserver is invoked serially after each failed account.
type AccountOperationFailureObserver func(AccountOperationFailure) error

// AccountOperationDetail formats a credential or quota failure for an admin toast.
// Unknown internal errors stay empty so callers can keep a stable fallback.
func AccountOperationDetail(err error) string {
	if err == nil {
		return ""
	}
	status, code, detail := classifyAccountOperationError(err)
	if status == 0 && code == "" && detail == "" {
		return ""
	}
	parts := make([]string, 0, 3)
	if status > 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", status))
	}
	if code != "" && !strings.Contains(detail, code) {
		parts = append(parts, code)
	}
	if detail != "" {
		parts = append(parts, detail)
	}
	return strings.Join(parts, " · ")
}

func classifyAccountOperationError(err error) (status int, code, detail string) {
	var refresh *provider.CredentialRefreshError
	if errors.As(err, &refresh) {
		detail = strings.TrimSpace(refresh.Message)
		if response := strings.TrimSpace(refresh.Response); response != "" {
			if detail != "" {
				detail += "\n"
			}
			detail += response
		}
		return refresh.Status, strings.TrimSpace(refresh.Code), truncateAccountOperationDetail(detail)
	}
	if errors.Is(err, ErrCredentialRefreshPermanent) || errors.Is(err, provider.ErrUnauthorized) {
		code = "credential_refresh_permanent"
		if errors.Is(err, provider.ErrUnauthorized) {
			code = "unauthorized"
		}
		return 0, code, truncateAccountOperationDetail(err.Error())
	}
	detail = truncateAccountOperationDetail(err.Error())
	if detail == "" || strings.Contains(strings.ToLower(detail), "bearer ") {
		return 0, "", ""
	}
	return 0, "", detail
}

func accountOperationStage(operation string, err error) string {
	var refresh *provider.CredentialRefreshError
	if errors.As(err, &refresh) || errors.Is(err, ErrCredentialRefreshPermanent) {
		return "credential_refresh"
	}
	switch operation {
	case "billing_sync":
		return "billing_sync"
	case "quota_sync", "web_quota_sync", "console_quota_sync":
		return "quota_sync"
	case "credential_refresh":
		return "credential_refresh"
	default:
		if operation == "" {
			return "credential_refresh"
		}
		return operation
	}
}

func truncateAccountOperationDetail(value string) string {
	value = strings.Map(func(char rune) rune {
		if char < 0x20 && char != '\n' && char != '\t' {
			return ' '
		}
		return char
	}, strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	const limit = 500
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}
