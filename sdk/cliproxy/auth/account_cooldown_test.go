package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

type fixedClock struct {
	now time.Time
}

func (c *fixedClock) Now() time.Time { return c.now }

func TestAccountCooldownHonorsRetryAfterAndMaximum(t *testing.T) {
	withQuotaCooldownEnabled(t)
	previousMax := maxCooldownSeconds.Load()
	SetMaxCooldownSeconds(30)
	t.Cleanup(func() { maxCooldownSeconds.Store(previousMax) })

	clock := &fixedClock{now: time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)}
	manager := NewManagerWithClock(nil, &RoundRobinSelector{}, nil, clock)
	manager.RegisterExecutor(codexOnlyFailureExecutor{})
	auth := &Auth{ID: "rate-limited-account", Provider: "codex"}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatal(errRegister)
	}

	retryAfter := 10 * time.Minute
	manager.MarkResult(context.Background(), Result{
		AuthID:     auth.ID,
		Error:      &Error{HTTPStatus: http.StatusTooManyRequests, Message: "rate limited"},
		RetryAfter: &retryAfter,
	})

	updated, _ := manager.GetByID(auth.ID)
	wantDeadline := clock.now.Add(30 * time.Second)
	if !updated.NextRetryAfter.Equal(wantDeadline) {
		t.Fatalf("NextRetryAfter = %v, want capped deadline %v", updated.NextRetryAfter, wantDeadline)
	}
	if _, errPick := manager.SelectAuth(context.Background(), auth.Provider, "", cliproxyexecutor.Options{}); errPick == nil {
		t.Fatal("SelectAuth during cooldown returned nil error")
	}
	clock.now = wantDeadline
	if picked, errPick := manager.SelectAuth(context.Background(), auth.Provider, "", cliproxyexecutor.Options{}); errPick != nil || picked.ID != auth.ID {
		t.Fatalf("SelectAuth at expiry = (%v, %v), want recovered account", picked, errPick)
	}
}

func TestAccountCooldownDoesNotStarveHealthyPoolAndSuccessRecovers(t *testing.T) {
	withQuotaCooldownEnabled(t)
	clock := &fixedClock{now: time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)}
	manager := NewManagerWithClock(nil, &RoundRobinSelector{}, nil, clock)
	manager.RegisterExecutor(codexOnlyFailureExecutor{})
	for _, id := range []string{"cooling", "healthy"} {
		if _, errRegister := manager.Register(WithSkipPersist(context.Background()), &Auth{ID: id, Provider: "codex"}); errRegister != nil {
			t.Fatal(errRegister)
		}
	}

	manager.MarkResult(context.Background(), quotaResult("cooling", ""))
	for i := 0; i < 8; i++ {
		picked, errPick := manager.SelectAuth(context.Background(), "codex", "", cliproxyexecutor.Options{})
		if errPick != nil || picked.ID != "healthy" {
			t.Fatalf("SelectAuth #%d = (%v, %v), want healthy", i, picked, errPick)
		}
	}

	manager.MarkResult(context.Background(), Result{AuthID: "cooling", Success: true})
	recovered, _ := manager.GetByID("cooling")
	if recovered.Unavailable || !recovered.NextRetryAfter.IsZero() || recovered.Quota.Exceeded {
		t.Fatalf("successful account retained cooldown: %+v", recovered)
	}
}

func TestAccountCooldownConcurrentSelectionIsRaceSafe(t *testing.T) {
	withQuotaCooldownEnabled(t)
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(codexOnlyFailureExecutor{})
	for _, id := range []string{"cooling-concurrent", "healthy-concurrent"} {
		if _, errRegister := manager.Register(WithSkipPersist(context.Background()), &Auth{ID: id, Provider: "codex"}); errRegister != nil {
			t.Fatal(errRegister)
		}
	}

	const workers = 64
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			manager.MarkResult(context.Background(), quotaResult("cooling-concurrent", ""))
			picked, errPick := manager.SelectAuth(context.Background(), "codex", "", cliproxyexecutor.Options{})
			if errPick != nil {
				errs <- errPick
				return
			}
			if picked.ID != "healthy-concurrent" {
				errs <- &Error{Message: "cooling account was selected concurrently"}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for errConcurrent := range errs {
		t.Error(errConcurrent)
	}
}

func TestAccountReloadClearsModelCooldown(t *testing.T) {
	withQuotaCooldownEnabled(t)
	clock := &fixedClock{now: time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)}
	manager := NewManagerWithClock(nil, nil, nil, clock)
	auth := &Auth{ID: "reloaded", Provider: "codex"}
	if _, errRegister := manager.Register(WithSkipPersist(context.Background()), auth); errRegister != nil {
		t.Fatal(errRegister)
	}
	manager.MarkResult(context.Background(), quotaResult(auth.ID, "gpt-5"))

	reloadCtx := WithAccountReload(WithSkipPersist(context.Background()))
	updated, errUpdate := manager.Update(reloadCtx, &Auth{ID: auth.ID, Provider: auth.Provider})
	if errUpdate != nil {
		t.Fatal(errUpdate)
	}
	if updated.Unavailable || !updated.NextRetryAfter.IsZero() || len(updated.ModelStates) != 0 {
		t.Fatalf("reloaded account retained cooldown state: %+v", updated)
	}
}
