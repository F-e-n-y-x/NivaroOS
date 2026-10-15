package service

import (
	"context"
	"sync"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/model"
	"github.com/F-e-n-y-x/NivaroOS/services/message-bus/repository"
	"gotest.tools/assert"
)

// Subscribe/Unsubscribe from request goroutines used to touch the
// subscriber map while Start's loop read it unlocked: under load that is
// Go's fatal "concurrent map read and map write" (run with -race).
func TestWSServicesConcurrentSubscribePublish(t *testing.T) {
	repo, err := repository.NewDatabaseRepositoryInMemory()
	assert.NilError(t, err)
	defer repo.Close()

	ets, ats := NewEventTypeService(&repo), NewActionTypeService(&repo)
	_, err = ets.RegisterEventType(model.EventType{SourceID: "s", Name: "e"})
	assert.NilError(t, err)
	_, err = ats.RegisterActionType(model.ActionType{SourceID: "s", Name: "a"})
	assert.NilError(t, err)

	ev, ac := NewEventServiceWS(ets), NewActionServiceWS(ats)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	go func() { ev.Start(&ctx); done <- struct{}{} }()
	go func() { ac.Start(&ctx); done <- struct{}{} }()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				ev.Publish(model.Event{SourceID: "s", Name: "e"})
				ac.Trigger(model.Action{SourceID: "s", Name: "a"})
				if c, err := ev.Subscribe("s", []string{"e"}); err == nil {
					_ = ev.Unsubscribe("s", "e", c)
				}
				if c, err := ac.Subscribe("s", []string{"a"}); err == nil {
					_ = ac.Unsubscribe("s", "a", c)
				}
			}
		}()
	}
	wg.Wait()
	cancel()
	<-done
	<-done
}
