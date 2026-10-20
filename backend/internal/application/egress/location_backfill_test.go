package egress

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	domain "github.com/chenyme/grok2api/backend/internal/domain/egress"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
	"github.com/chenyme/grok2api/backend/internal/repository"
)

type locationBackfillRepository struct {
	ServiceRepository
	nodes []domain.Node
}

func (r *locationBackfillRepository) ListEgressNodes(context.Context, domain.Scope, repository.SortQuery) ([]domain.Node, error) {
	return append([]domain.Node(nil), r.nodes...), nil
}

func (*locationBackfillRepository) SetEgressNodeLocationIfEmpty(context.Context, uint64, string) error {
	return nil
}

type locationBackfillProber struct{ seen map[string]bool }

func (*locationBackfillProber) ProbeEgressNode(context.Context, domain.Node) (domain.ProbeResult, error) {
	return domain.ProbeResult{}, nil
}

func (p *locationBackfillProber) LookupExitCountry(_ context.Context, ip string) string {
	p.seen[ip] = true
	return ""
}

func TestBackfillEmptyLocationsVisitsEveryScopeAfterRepeatedMisses(t *testing.T) {
	cipher, err := security.NewCipher(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	encryptedProxy, err := cipher.Encrypt("http://edge.example:8080")
	if err != nil {
		t.Fatal(err)
	}
	repository := &locationBackfillRepository{}
	for scopeIndex, scope := range []domain.Scope{domain.ScopeBuild, domain.ScopeConsole, domain.ScopeWeb} {
		base := []uint64{100, 1, 200}[scopeIndex]
		for offset := uint64(0); offset < 8; offset++ {
			id := base + offset
			repository.nodes = append(repository.nodes, domain.Node{
				ID: id, Name: "node", Scope: scope, EncryptedProxyURL: encryptedProxy,
				ExitIP: fmt.Sprintf("203.0.113.%d", id),
			})
		}
	}
	prober := &locationBackfillProber{seen: make(map[string]bool)}
	service := NewService(repository, cipher, "")
	service.SetNodeProber(prober)
	for cycle := 0; cycle < 9; cycle++ {
		if err := service.backfillEmptyLocations(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(prober.seen) != len(repository.nodes) {
		t.Fatalf("backfill visited %d/%d nodes after repeated misses", len(prober.seen), len(repository.nodes))
	}
}
