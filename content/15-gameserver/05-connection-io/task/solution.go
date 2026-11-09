package task

import (
	"errors"
	"io"
	"sync"
)

var (
	ErrSlowClient = errors.New("slow client: outgoing queue is full")
	ErrClosed     = errors.New("client closed")
)

// Client — исходящая сторона соединения: ограниченная очередь
// и горутина-писатель.
type Client struct {
	conn    io.WriteCloser
	queue   chan []byte
	quit    chan struct{} // закрывается в Close
	done    chan struct{} // закрывается, когда писатель вышел
	onClose func(reason error)

	mu  sync.Mutex
	err error
}

func NewClient(conn io.WriteCloser, queue int, onClose func(reason error)) *Client {
	if queue < 1 {
		queue = 1
	}
	c := &Client{
		conn:    conn,
		queue:   make(chan []byte, queue),
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
		onClose: onClose,
	}
	go c.writeLoop()
	return c
}

// Send не ждёт никогда. Канал очереди не закрывается ни в каком
// случае — поэтому Send, одновременный с Close, не паникует.
func (c *Client) Send(msg []byte) bool {
	select {
	case <-c.quit:
		return false
	default:
	}
	select {
	case c.queue <- msg:
		return true
	default:
		c.Close(ErrSlowClient)
		return false
	}
}

func (c *Client) Close(reason error) {
	if reason == nil {
		reason = ErrClosed
	}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = reason
	close(c.quit)
	c.mu.Unlock()
	c.conn.Close() // будит писателя, если он висит в Write
}

func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Client) writeLoop() {
	defer close(c.done)
	defer func() {
		if c.onClose != nil {
			c.onClose(c.Err())
		}
	}()
	for {
		select {
		case <-c.quit:
			return
		case msg := <-c.queue:
			// select выбирает случайно среди готовых: закрытие
			// проверяем ещё раз, чтобы не писать после Close.
			select {
			case <-c.quit:
				return
			default:
			}
			if _, err := c.conn.Write(msg); err != nil {
				c.Close(err)
				return
			}
		}
	}
}
