package firstrun_test

import (
	"sync"
	"testing"

	"github.com/koduj-dev/context-commander/internal/firstrun"
)

func TestCodeIsSingleUse(t *testing.T) {
	code, err := firstrun.NewCode()
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}
	value := code.String()
	if len(value) != 32 {
		t.Errorf("code is %d hex characters, want 32 (128 bits)", len(value))
	}
	if code.Spent() {
		t.Error("a fresh code reports itself spent")
	}
	if !code.Consume(value) {
		t.Fatal("the correct code was refused")
	}
	if !code.Spent() {
		t.Error("a consumed code does not report itself spent")
	}
	// The second attempt is the one that matters: setup creates an
	// installation admin, so a replayable code is a second admin.
	if code.Consume(value) {
		t.Error("the same code was accepted twice")
	}
}

func TestCodeRefusesWrongValues(t *testing.T) {
	code, err := firstrun.NewCode()
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}
	value := code.String()
	for name, presented := range map[string]string{
		"empty":     "",
		"truncated": value[:len(value)-1],
		"extended":  value + "0",
		"different": "00000000000000000000000000000000",
	} {
		if code.Consume(presented) {
			t.Errorf("%s: accepted", name)
		}
	}
	// None of those may have spent it.
	if !code.Consume(value) {
		t.Error("a failed attempt consumed the code")
	}
}

func TestNilCodeAcceptsNothing(t *testing.T) {
	// What an installation that already has users gets. It must refuse
	// rather than panic, and must report itself spent so no caller treats
	// the absence of a code as permission.
	var code *firstrun.Code
	if code.Consume("") || code.Consume("anything") {
		t.Error("a nil code accepted something")
	}
	if !code.Spent() {
		t.Error("a nil code does not report itself spent")
	}
	if code.String() != "" {
		t.Error("a nil code returned a value")
	}
}

func TestOnlyOneConcurrentConsumerWins(t *testing.T) {
	code, err := firstrun.NewCode()
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}
	value := code.String()

	const racers = 32
	var wg sync.WaitGroup
	results := make([]bool, racers)
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = code.Consume(value)
		}()
	}
	close(start)
	wg.Wait()

	won := 0
	for _, ok := range results {
		if ok {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d of %d concurrent attempts succeeded, want exactly 1", won, racers)
	}
}
