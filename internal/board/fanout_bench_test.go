package board

import (
	"fmt"
	"testing"
)

// One reader keeps up while the other sessions stop draining chat events.
func BenchmarkChatWithSlowReaders(b *testing.B) {
	for _, n := range []int{0, 16, 128} {
		b.Run(fmt.Sprintf("%d-stalled", n), func(b *testing.B) {
			broker := NewBroker()
			active, closeActive := broker.Subscribe()
			defer closeActive()
			for range n {
				_, unsubscribe := broker.Subscribe()
				defer unsubscribe()
			}
			event := Event{Kind: EventChat, BoardID: 1}
			for range cap(active) {
				broker.Publish(event)
				<-active
			}
			b.ReportAllocs()
			for b.Loop() {
				broker.Publish(event)
				if got := <-active; got != event {
					b.Fatal("active reader missed chat event")
				}
			}
		})
	}
}
