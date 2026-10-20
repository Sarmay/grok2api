package relational

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	egressapp "github.com/chenyme/grok2api/backend/internal/application/egress"
	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

func TestSubscriptionSyncUsesStoredLocationForHongKongExclusion(t *testing.T) {
	ctx := context.Background()
	database := openTestDatabase(t)
	accounts := NewAccountRepository(database)
	nodes := NewEgressRepository(database)
	cipher := egressOperationsCipher(t)
	feed := "trojan://password@edge.example:443\ntrojan://password@other.example:443\n"
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, feed)
	}))
	defer proxyServer.Close()
	encryptedURL, err := cipher.Encrypt("http://1.1.1.1/subscription")
	if err != nil {
		t.Fatal(err)
	}
	encryptedFetchProxy, err := cipher.Encrypt(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	source, err := nodes.CreateEgressSource(ctx, egress.SubscriptionSource{
		Name: "unknown-source", Scope: egress.ScopeBuild, Enabled: true, EncryptedURL: encryptedURL,
		EncryptedProxyURL: encryptedFetchProxy, RefreshIntervalSeconds: 900, ExcludeHongKong: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	service := egressapp.NewService(nodes, cipher, "test-browser", accounts)
	if _, err := service.SyncSource(ctx, source.ID); err != nil {
		t.Fatal(err)
	}
	listed, err := nodes.ListEgressNodes(ctx, "", repository.SortQuery{})
	if err != nil || len(listed) != 2 {
		t.Fatalf("initial sync nodes=%d err=%v", len(listed), err)
	}
	for _, node := range listed {
		if err := nodes.SetEgressNodeLocationIfEmpty(ctx, node.ID, "HK"); err != nil {
			t.Fatal(err)
		}
	}
	bound := listed[0]
	unboundID := listed[1].ID
	credential := createEgressOperationsAccount(t, ctx, accounts, "hk-bound-account")
	if _, err := accounts.UpdateEgressBindings(ctx, account.ProviderBuild, []uint64{credential.ID}, &bound.ID, account.EgressAssignmentAuto, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	result, err := service.SyncSource(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != 2 {
		t.Fatalf("excluded entries = %d, want 2", result.Skipped)
	}
	listed, err = nodes.ListEgressNodes(ctx, "", repository.SortQuery{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("after exclusion nodes=%d err=%v", len(listed), err)
	}
	if listed[0].ID != bound.ID || listed[0].Enabled {
		t.Fatalf("bound Hong Kong node = %#v, want retained and disabled", listed[0])
	}
	if listed[0].ID == unboundID {
		t.Fatal("unbound Hong Kong node was retained")
	}
	storedAccount, err := accounts.Get(ctx, credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedAccount.EgressNodeID != bound.ID || storedAccount.EgressAssignmentMode != account.EgressAssignmentAuto {
		t.Fatalf("bound account assignment changed: %#v", storedAccount)
	}
}

func TestSubscriptionSyncLocationExclusionRespectsToggleLabelsAndSource(t *testing.T) {
	ctx := context.Background()
	database := openTestDatabase(t)
	nodes := NewEgressRepository(database)
	cipher := egressOperationsCipher(t)
	var feed string
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, feed)
	}))
	defer proxyServer.Close()
	encryptedURL, err := cipher.Encrypt("http://1.1.1.1/subscription")
	if err != nil {
		t.Fatal(err)
	}
	encryptedFetchProxy, err := cipher.Encrypt(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	createSource := func(name string, exclude bool) egress.SubscriptionSource {
		t.Helper()
		source, createErr := nodes.CreateEgressSource(ctx, egress.SubscriptionSource{
			Name: name, Scope: egress.ScopeBuild, Enabled: true, EncryptedURL: encryptedURL,
			EncryptedProxyURL: encryptedFetchProxy, RefreshIntervalSeconds: 900, ExcludeHongKong: exclude,
		})
		if createErr != nil {
			t.Fatal(createErr)
		}
		return source
	}
	service := egressapp.NewService(nodes, cipher, "test-browser")
	runFeed := func(source egress.SubscriptionSource, value string) egressapp.ImportResult {
		t.Helper()
		feed = value
		result, syncErr := service.SyncSource(ctx, source.ID)
		if syncErr != nil {
			t.Fatal(syncErr)
		}
		return result
	}
	findNode := func(sourceID uint64) (egress.Node, bool) {
		t.Helper()
		listed, listErr := nodes.ListEgressNodes(ctx, "", repository.SortQuery{})
		if listErr != nil {
			t.Fatal(listErr)
		}
		for _, node := range listed {
			if node.SourceID == sourceID {
				return node, true
			}
		}
		return egress.Node{}, false
	}
	knownProxy := "trojan://password@shared.example:443"
	filteredSource := createSource("filter-source", true)
	runFeed(filteredSource, knownProxy+"\n")
	filteredNode, ok := findNode(filteredSource.ID)
	if !ok {
		t.Fatal("initial filtered node was not imported")
	}
	if err := nodes.SetEgressNodeLocationIfEmpty(ctx, filteredNode.ID, "HK"); err != nil {
		t.Fatal(err)
	}
	if result := runFeed(filteredSource, knownProxy+"#Tokyo\n"); result.Skipped != 0 {
		t.Fatalf("explicit non-HK label skipped %d nodes", result.Skipped)
	}
	updated, ok := findNode(filteredSource.ID)
	if !ok || updated.Location != "JP" || !updated.Enabled {
		t.Fatalf("explicit non-HK label node = %#v, found=%t", updated, ok)
	}

	offSource := createSource("filter-off-source", false)
	runFeed(offSource, knownProxy+"\n")
	offNode, ok := findNode(offSource.ID)
	if !ok {
		t.Fatal("filter-off node was not imported")
	}
	if err := nodes.SetEgressNodeLocationIfEmpty(ctx, offNode.ID, "HK"); err != nil {
		t.Fatal(err)
	}
	result := runFeed(offSource, knownProxy+"\n")
	if result.Skipped != 0 {
		t.Fatalf("filter-off sync skipped %d nodes", result.Skipped)
	}
	if retained, ok := findNode(offSource.ID); !ok || !retained.Enabled {
		t.Fatalf("filter-off HK node retained=%t node=%#v", ok, retained)
	}

	// A same-key node from another source must not lend its HK location.
	crossSource := createSource("cross-source", true)
	result = runFeed(crossSource, knownProxy+"\n")
	if result.Skipped != 0 {
		t.Fatalf("cross-source entry skipped %d nodes", result.Skipped)
	}
	if _, ok := findNode(crossSource.ID); !ok {
		t.Fatal("same-key node was incorrectly excluded using another source's location")
	}
}
