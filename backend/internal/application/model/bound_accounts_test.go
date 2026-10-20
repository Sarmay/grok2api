package model

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

func TestValidateBoundAccountsFindsAccountsOutsideDefaultPage(t *testing.T) {
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "bound-accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accounts := relational.NewAccountRepository(database)
	values := make([]account.Credential, 1001)
	for index := range values {
		values[index] = account.Credential{
			Provider: account.ProviderBuild, Name: fmt.Sprintf("account-%04d", index),
			SourceKey: fmt.Sprintf("binding-%04d", index), EncryptedAccessToken: "encrypted",
			AuthStatus: account.AuthStatusActive, ExpiresAt: time.Now().Add(time.Hour),
		}
	}
	created, err := accounts.UpsertManyByIdentity(ctx, values)
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := accounts.List(ctx, repository.AccountListQuery{
		Page: repository.PageQuery{Limit: 1000}, Filter: repository.AccountListFilter{Provider: string(account.ProviderBuild)},
	})
	if err != nil {
		t.Fatal(err)
	}
	onPage := make(map[uint64]bool, len(page))
	for _, value := range page {
		onPage[value.ID] = true
	}
	var targetID uint64
	for _, value := range created {
		if !onPage[value.ID] {
			targetID = value.ID
			break
		}
	}
	if targetID == 0 {
		t.Fatal("fixture must include an account outside the default page")
	}
	service := NewService(nil, accounts, nil, nil)
	bound, err := service.validateBoundAccounts(ctx, account.ProviderBuild, []uint64{targetID, targetID})
	if err != nil || len(bound) != 1 || bound[0] != targetID {
		t.Fatalf("binding outside default page = %v, err = %v", bound, err)
	}
	if _, err := service.validateBoundAccounts(ctx, account.ProviderWeb, []uint64{targetID}); err == nil {
		t.Fatal("cross-provider binding was accepted")
	}
	if _, err := service.validateBoundAccounts(ctx, account.ProviderBuild, []uint64{created[len(created)-1].ID + 1}); err == nil {
		t.Fatal("missing account binding was accepted")
	}
}
