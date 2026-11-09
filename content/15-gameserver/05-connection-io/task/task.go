package task

import (
	"errors"
	"io"
)

var (
	ErrSlowClient = errors.New("slow client: outgoing queue is full")
	ErrClosed     = errors.New("client closed")
)

// Client — исходящая сторона соединения: ограниченная очередь
// и горутина-писатель.
type Client struct {
	// TODO: соединение, очередь, сигнал закрытия, причина, onClose
}

func NewClient(conn io.WriteCloser, queue int, onClose func(reason error)) *Client {
	// TODO: создать очередь и запустить горутину-писатель
	return &Client{}
}

func (c *Client) Send(msg []byte) bool {
	// TODO: никогда не ждать; нет места — отключить клиента
	return false
}

func (c *Client) Close(reason error) {
	// TODO: ровно один раз: причина, закрыть conn, разбудить писателя
}

func (c *Client) Done() <-chan struct{} {
	// TODO
	return nil
}

func (c *Client) Err() error {
	// TODO
	return nil
}
