package inference

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	accountapp "github.com/chenyme/grok2api/backend/internal/application/account"
	clientkeyapp "github.com/chenyme/grok2api/backend/internal/application/clientkey"
	"github.com/chenyme/grok2api/backend/internal/application/gateway"
	"github.com/chenyme/grok2api/backend/internal/domain/account"
	"github.com/chenyme/grok2api/backend/internal/domain/audit"
	"github.com/chenyme/grok2api/backend/internal/domain/clientkey"
	modeldomain "github.com/chenyme/grok2api/backend/internal/domain/model"
	"github.com/chenyme/grok2api/backend/internal/infra/persistence/relational"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"github.com/chenyme/grok2api/backend/internal/infra/runtime/memory"
	"github.com/chenyme/grok2api/backend/internal/infra/security"
	"github.com/chenyme/grok2api/backend/internal/repository"
	"github.com/chenyme/grok2api/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type handlerVoiceAdapter struct{ conn *handlerVoiceConn }

func (*handlerVoiceAdapter) Provider() account.Provider { return account.ProviderConsole }
func (*handlerVoiceAdapter) Definition() provider.Definition {
	return provider.Definition{Provider: account.ProviderConsole, Media: provider.MediaSurface{STT: true}}
}
func (a *handlerVoiceAdapter) DialVoiceWebSocket(context.Context, provider.VoiceWebSocketRequest) (provider.VoiceWebSocketConn, func(), error) {
	return a.conn, func() {}, nil
}

type handlerVoiceConn struct {
	readStarted chan struct{}
	closed      chan struct{}
	releaseRead chan struct{}
	returnEvent bool
	closeOnce   sync.Once
	readOnce    sync.Once
	mu          sync.Mutex
	reads       int
}

func newHandlerVoiceConn(returnEvent bool) *handlerVoiceConn {
	return &handlerVoiceConn{
		readStarted: make(chan struct{}), closed: make(chan struct{}),
		releaseRead: make(chan struct{}), returnEvent: returnEvent,
	}
}

func (c *handlerVoiceConn) ReadMessage() (int, []byte, error) {
	c.mu.Lock()
	c.reads++
	readNumber := c.reads
	c.mu.Unlock()
	if readNumber == 1 {
		c.readOnce.Do(func() { close(c.readStarted) })
		if c.returnEvent {
			<-c.releaseRead
			return websocket.TextMessage, []byte(`{"type":"transcript.done","duration":3.45}`), nil
		}
		<-c.closed
		return 0, nil, errors.New("upstream connection closed")
	}
	<-c.closed
	return 0, nil, errors.New("upstream connection closed")
}

func (*handlerVoiceConn) WriteMessage(int, []byte) error { return nil }
func (*handlerVoiceConn) SetReadLimit(int64)             {}
func (c *handlerVoiceConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

type handlerVoiceFixture struct {
	server      *httptest.Server
	clientKey   clientkey.Key
	clientKeys  *relational.ClientKeyRepository
	audits      *relational.AuditRepository
	limiter     *memory.ConcurrencyLimiter
	accountID   uint64
	conn        *handlerVoiceConn
	handlerDone chan struct{}
}

func newHandlerVoiceFixture(t *testing.T, conn *handlerVoiceConn, requestTimeout time.Duration) *handlerVoiceFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	database, err := relational.OpenSQLite(ctx, filepath.Join(t.TempDir(), "voice-handler.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.InitializeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	accountRepo := relational.NewAccountRepository(database)
	modelRepo := relational.NewModelRepository(database)
	audits := relational.NewAuditRepository(database)
	keyRepo := relational.NewClientKeyRepository(database)
	credential, _, err := accountRepo.UpsertByIdentity(ctx, account.Credential{
		Provider: account.ProviderConsole, AuthType: account.AuthTypeSSO,
		Name: "voice-handler-console", SourceKey: "voice-handler-console", EncryptedAccessToken: "encrypted",
		Enabled: true, AuthStatus: account.AuthStatusActive, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	const model = "handler-stt"
	if err := modelRepo.UpsertRoutes(ctx, []modeldomain.Route{{PublicID: model, Provider: account.ProviderConsole, UpstreamModel: model, Capability: modeldomain.CapabilitySTT, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := modelRepo.ReplaceAccountCapabilities(ctx, credential.ID, []string{model}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	clientKey, err := keyRepo.Create(ctx, clientkey.Key{
		Name: "voice-handler-key", Prefix: "voice-handler-key",
		SecretHash: strings.Repeat("0", 64), EncryptedSecret: "encrypted", Enabled: true,
		BillingLimitUSDTicks: 10_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 32)
	cipher, err := security.NewCipher(base64.StdEncoding.EncodeToString(secret))
	if err != nil {
		t.Fatal(err)
	}
	adapter := &handlerVoiceAdapter{conn: conn}
	registry := provider.NewRegistry(adapter)
	sticky := memory.NewStickyStore()
	limiter := memory.NewConcurrencyLimiter()
	selector := gateway.NewSelector(accountRepo, limiter, sticky, registry, time.Hour, time.Second, time.Minute)
	accounts := accountapp.NewService(accountRepo, audits, memory.NewDeviceSessionStore(), sticky, registry, cipher, nil)
	keys := clientkeyapp.NewService(keyRepo, nil, nil, 60, 4, cipher)
	service := gateway.NewService(modelRepo, audits, accounts, keys, registry, selector, nil, 1)
	handler := NewHandler(service, nil, 1<<20)
	router := gin.New()
	handlerDone := make(chan struct{})
	router.Use(func(c *gin.Context) {
		c.Set(middleware.ClientKey, clientKey)
		c.Set(middleware.RequestIDKey, "voice-handler-request")
		requestCtx, cancel := context.WithTimeout(c.Request.Context(), requestTimeout)
		defer cancel()
		c.Request = c.Request.WithContext(requestCtx)
		c.Next()
		close(handlerDone)
	})
	handler.Register(router.Group("/v1"))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	t.Cleanup(func() {
		_ = conn.Close()
		select {
		case <-conn.releaseRead:
		default:
			close(conn.releaseRead)
		}
	})
	return &handlerVoiceFixture{
		server: server, clientKey: clientKey, clientKeys: keyRepo, audits: audits,
		limiter: limiter, accountID: credential.ID, conn: conn, handlerDone: handlerDone,
	}
}

func (f *handlerVoiceFixture) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(f.server.URL, "http") + "/v1/stt?model=handler-stt"
	conn, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{f.server.URL}})
	if err != nil {
		if response != nil {
			t.Fatalf("websocket dial failed: %v (status %s)", err, response.Status)
		}
		t.Fatal(err)
	}
	return conn
}

func (f *handlerVoiceFixture) assertFinalized(t *testing.T, wantError string, wantDuration float64) {
	t.Helper()
	records, total, err := f.audits.List(context.Background(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(records) != 1 {
		t.Fatalf("audit count = %d/%d, want one record", len(records), total)
	}
	if records[0].ErrorCode != wantError {
		t.Fatalf("audit error code = %q, want %q", records[0].ErrorCode, wantError)
	}
	key, err := f.clientKeys.Get(context.Background(), f.clientKey.ID)
	if err != nil {
		t.Fatal(err)
	}
	if wantDuration == 0 {
		if key.BilledUsageUSDTicks != 0 {
			t.Fatalf("billed usage = %d, want 0", key.BilledUsageUSDTicks)
		}
	} else {
		pricing, ok := audit.EstimateOfficialSTTCost(wantDuration, true)
		if !ok || key.BilledUsageUSDTicks != pricing.CostInUSDTicks {
			t.Fatalf("billed usage = %d, want %d for %.2fs", key.BilledUsageUSDTicks, pricing.CostInUSDTicks, wantDuration)
		}
	}
	active, err := f.limiter.Current(context.Background(), repository.AccountConcurrencyKey(f.accountID))
	if err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("active account leases = %d, want 0", active)
	}
}

func waitForVoiceSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func TestVoiceWSHandlerQuietConnectionDeadlineFinalizesAndReleasesLease(t *testing.T) {
	fixture := newHandlerVoiceFixture(t, newHandlerVoiceConn(false), 250*time.Millisecond)
	client := fixture.dial(t)
	defer client.Close()
	waitForVoiceSignal(t, fixture.conn.readStarted, "upstream read")
	waitForVoiceSignal(t, fixture.handlerDone, "handler deadline exit")
	waitForVoiceSignal(t, fixture.conn.closed, "upstream connection close")
	fixture.assertFinalized(t, "request_timeout", 0)
}

func TestVoiceWSHandlerWaitsForReadTranscriptBeforeClientDisconnectBilling(t *testing.T) {
	fixture := newHandlerVoiceFixture(t, newHandlerVoiceConn(true), 10*time.Second)
	client := fixture.dial(t)
	waitForVoiceSignal(t, fixture.conn.readStarted, "upstream transcript read")
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	waitForVoiceSignal(t, fixture.conn.closed, "upstream connection close")
	select {
	case <-fixture.handlerDone:
		t.Fatal("handler finalized before the in-flight transcript read completed")
	default:
	}
	close(fixture.conn.releaseRead)
	waitForVoiceSignal(t, fixture.handlerDone, "handler disconnect exit")
	fixture.assertFinalized(t, "client_stream_interrupted", 3.45)
}
