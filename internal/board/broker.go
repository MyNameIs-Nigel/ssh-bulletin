package board

import "sync"

// EventKind names what changed.
type EventKind int

const (
	EventThread     EventKind = iota + 1 // a new thread (and its first post)
	EventReply                           // a reply in an existing thread
	EventKudos                           // kudos changed on a post
	EventProfile                         // an account changed its handle/palette
	EventRole                            // an account gained or lost admin access
	EventReview                          // the private review queue or history changed
	EventModeration                      // a thread or post changed visibility
)

// Event is broadcast to every live session so screens refresh in place.
type Event struct {
	Kind      EventKind
	AccountID int64
	BoardID   int64
	ThreadID  int64
	PostID    int64
	Actor     string // handle of whoever caused it
	Title     string // thread title, for toasts
}

// Broker fans events out to subscribers. Delivery is best-effort: a slow
// session's buffer fills and it misses events rather than stalling posters.
type Broker struct {
	mu   sync.Mutex
	next int
	subs map[int]chan Event
}

// NewBroker returns an empty broker.
func NewBroker() *Broker { return &Broker{subs: map[int]chan Event{}} }

// Subscribe registers a listener. Call the returned func to unsubscribe;
// it closes the channel.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, 32)
	b.subs[id] = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.subs, id)
			close(ch)
		})
	}
}

// Publish delivers e to every subscriber without blocking.
func (b *Broker) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Subscribers is the number of live sessions listening.
func (b *Broker) Subscribers() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}
